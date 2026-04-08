use serde_json::Value;
use std::env;
use std::error::Error;
use std::ffi::OsString;
use std::fmt;
use std::fs;
use std::io::{self, BufRead, BufReader, Read, Write};
use std::net::{TcpStream, ToSocketAddrs};
use std::path::PathBuf;
use std::process::{Child, Command, ExitStatus, Stdio};
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant};
use tauri::{App, AppHandle, Manager, RunEvent, Runtime};
use url::Url;

const DEFAULT_GO_SIDECAR_BASE_URL: &str = "http://127.0.0.1:38181";
const BUILT_GO_SIDECAR_URL_ENV_KEY: &str = "XRAYVIEW_FRONTEND_GO_BACKEND_URL";
const GO_SIDECAR_BINARY_NAME: &str = "xrayview-go-backend";
const EXPECTED_SERVICE_NAME: &str = "xrayview-go-backend";
const EXPECTED_TRANSPORT_KIND: &str = "local-http-json";
const STARTUP_TIMEOUT: Duration = Duration::from_secs(10);
const SHUTDOWN_GRACE_PERIOD: Duration = Duration::from_secs(5);
const PROBE_TIMEOUT: Duration = Duration::from_millis(350);
const PROBE_INTERVAL: Duration = Duration::from_millis(125);
const TEMP_ROOT_DIR_NAME: &str = "xrayview";
const LEGACY_APP_PERSISTENCE_DIR_NAME: &str = "go-backend";

#[derive(Default)]
pub struct GoBackendSidecarState {
    managed: Mutex<Option<ManagedGoBackend>>,
}

#[derive(Debug)]
struct ManagedGoBackend {
    child: Child,
    base_url: GoSidecarBaseUrl,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct GoSidecarBaseUrl {
    raw: String,
    host: String,
    port: u16,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum RuntimeMode {
    Mock,
    LegacyRust,
    GoSidecar,
}

impl RuntimeMode {
    fn requires_go_sidecar(self) -> bool {
        matches!(self, Self::LegacyRust | Self::GoSidecar)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum ProbeResult {
    Unreachable,
    ExpectedGoBackend,
    ReachableUnexpected,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct SidecarConfigWarning(String);

impl fmt::Display for SidecarConfigWarning {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl Error for SidecarConfigWarning {}

pub fn setup<R: Runtime>(app: &mut App<R>) -> Result<(), Box<dyn Error>> {
    let runtime_mode = resolve_runtime_mode();
    if !runtime_mode.requires_go_sidecar() {
        return Ok(());
    }

    let (base_url, warning) = resolve_go_sidecar_base_url();
    if let Some(warning) = warning {
        eprintln!("[xrayview] go sidecar: {warning}");
    }

    match probe_sidecar(&base_url, PROBE_TIMEOUT) {
        ProbeResult::ExpectedGoBackend => {
            return Err(io::Error::other(format!(
                "go sidecar startup aborted because {} is already served by another {} process; stop the stale sidecar and relaunch the app",
                base_url.raw, EXPECTED_SERVICE_NAME
            ))
            .into());
        }
        ProbeResult::ReachableUnexpected => {
            return Err(io::Error::other(format!(
                "go sidecar startup aborted because {} is already in use by another local service",
                base_url.raw
            ))
            .into());
        }
        ProbeResult::Unreachable => {}
    }

    let binary_path = resolve_sidecar_binary_path(app.handle())?;
    let backend_root_dir = app.path().temp_dir()?.join(TEMP_ROOT_DIR_NAME);
    let cache_dir = backend_root_dir.join("cache");
    let persistence_dir = backend_root_dir.join("state");
    migrate_legacy_catalog_if_needed(app, &persistence_dir);
    let mut command = Command::new(&binary_path);
    command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .env("XRAYVIEW_GO_BACKEND_HOST", &base_url.host)
        .env("XRAYVIEW_GO_BACKEND_PORT", base_url.port.to_string())
        .env("XRAYVIEW_GO_BACKEND_BASE_DIR", &backend_root_dir);

    let mut child = command.spawn().map_err(|error| {
        io::Error::other(format!(
            "failed to start go sidecar {}: {error}",
            binary_path.display()
        ))
    })?;
    pipe_child_output(&mut child, "stdout");
    pipe_child_output(&mut child, "stderr");

    wait_for_sidecar_ready(&mut child, &base_url)?;
    if runtime_mode == RuntimeMode::LegacyRust {
        eprintln!(
            "[xrayview] legacy-rust desktop runtime is deprecated and remains available only as a temporary fallback"
        );
        eprintln!(
            "[xrayview] go sidecar enabled for Go-owned openStudy, processStudy, analyzeStudy, and manual line measurement while legacy-rust remains selected"
        );
    }
    eprintln!(
        "[xrayview] go sidecar ready at {} (root={}, cache={}, persistence={})",
        base_url.raw,
        backend_root_dir.display(),
        cache_dir.display(),
        persistence_dir.display()
    );

    let state = app.state::<GoBackendSidecarState>();
    let mut managed = state
        .managed
        .lock()
        .map_err(|_| io::Error::other("go sidecar state mutex is unavailable"))?;
    *managed = Some(ManagedGoBackend { child, base_url });

    Ok(())
}

fn migrate_legacy_catalog_if_needed<R: Runtime>(app: &App<R>, persistence_dir: &PathBuf) {
    let Ok(legacy_dir) = app.path().app_local_data_dir() else {
        return;
    };

    let legacy_catalog_path = legacy_dir
        .join(LEGACY_APP_PERSISTENCE_DIR_NAME)
        .join("catalog.json");
    let target_catalog_path = persistence_dir.join("catalog.json");
    if !legacy_catalog_path.is_file() || target_catalog_path.exists() {
        return;
    }

    if let Err(error) = fs::create_dir_all(persistence_dir)
        .and_then(|_| fs::copy(&legacy_catalog_path, &target_catalog_path).map(|_| ()))
    {
        eprintln!(
            "[xrayview] go sidecar: failed to migrate catalog from {} to {}: {}",
            legacy_catalog_path.display(),
            target_catalog_path.display(),
            error
        );
        return;
    }

    eprintln!(
        "[xrayview] go sidecar: migrated recent-study catalog from {} to {}",
        legacy_catalog_path.display(),
        target_catalog_path.display()
    );
}

pub fn handle_run_event<R: Runtime>(app: &AppHandle<R>, event: &RunEvent) {
    if matches!(event, RunEvent::Exit) {
        shutdown_sidecar(app);
    }
}

fn shutdown_sidecar<R: Runtime>(app: &AppHandle<R>) {
    let state = app.state::<GoBackendSidecarState>();
    let managed = state.managed.lock().ok().and_then(|mut guard| guard.take());
    let Some(mut managed) = managed else {
        return;
    };

    eprintln!("[xrayview] stopping go sidecar at {}", managed.base_url.raw);
    terminate_child(&mut managed.child);
}

fn resolve_runtime_mode() -> RuntimeMode {
    runtime_mode_from_raw(option_env!("XRAYVIEW_FRONTEND_BACKEND_RUNTIME").unwrap_or_default())
}

fn runtime_mode_from_raw(value: &str) -> RuntimeMode {
    match value.trim().to_ascii_lowercase().as_str() {
        "mock" => RuntimeMode::Mock,
        "legacy-rust" => RuntimeMode::LegacyRust,
        "go-sidecar" => RuntimeMode::GoSidecar,
        _ => RuntimeMode::GoSidecar,
    }
}

fn resolve_go_sidecar_base_url() -> (GoSidecarBaseUrl, Option<SidecarConfigWarning>) {
    let raw_value = option_env!("XRAYVIEW_FRONTEND_GO_BACKEND_URL")
        .unwrap_or_default()
        .trim()
        .to_string();
    resolve_go_sidecar_base_url_from_raw(Some(raw_value))
}

fn resolve_go_sidecar_base_url_from_raw(
    raw_value: Option<String>,
) -> (GoSidecarBaseUrl, Option<SidecarConfigWarning>) {
    let normalized = raw_value.unwrap_or_default();
    if normalized.trim().is_empty() {
        return (default_go_sidecar_base_url(), None);
    }

    match parse_go_sidecar_base_url(normalized.trim()) {
        Ok(base_url) => (base_url, None),
        Err(reason) => (
            default_go_sidecar_base_url(),
            Some(SidecarConfigWarning(format!(
                "{BUILT_GO_SIDECAR_URL_ENV_KEY} must be an absolute loopback http URL \
                 (for example {DEFAULT_GO_SIDECAR_BASE_URL}). Falling back to \
                 {DEFAULT_GO_SIDECAR_BASE_URL}. ({reason})"
            ))),
        ),
    }
}

fn default_go_sidecar_base_url() -> GoSidecarBaseUrl {
    GoSidecarBaseUrl {
        raw: DEFAULT_GO_SIDECAR_BASE_URL.to_string(),
        host: "127.0.0.1".to_string(),
        port: 38181,
    }
}

fn parse_go_sidecar_base_url(raw: &str) -> Result<GoSidecarBaseUrl, String> {
    let parsed = Url::parse(raw).map_err(|error| error.to_string())?;
    if parsed.scheme() != "http" {
        return Err(format!("unsupported protocol: {}", parsed.scheme()));
    }

    let host = parsed
        .host_str()
        .ok_or_else(|| "host is required".to_string())?
        .to_string();
    if host != "localhost" && host != "127.0.0.1" && host != "::1" {
        return Err(format!(
            "host must be localhost, 127.0.0.1, or [::1]: {host}"
        ));
    }

    if parsed.path() != "/" || parsed.query().is_some() || parsed.fragment().is_some() {
        return Err("URL must not include a path, query, hash, or credentials".to_string());
    }

    if !parsed.username().is_empty() || parsed.password().is_some() {
        return Err("URL must not include a path, query, hash, or credentials".to_string());
    }

    Ok(GoSidecarBaseUrl {
        raw: raw.trim_end_matches('/').to_string(),
        host,
        port: parsed.port().unwrap_or(80),
    })
}

fn resolve_sidecar_binary_path<R: Runtime>(app: &AppHandle<R>) -> Result<PathBuf, Box<dyn Error>> {
    let file_name = sidecar_binary_file_name();
    let mut candidates = Vec::new();

    if let Ok(exe_path) = env::current_exe() {
        if let Some(exe_dir) = exe_path.parent() {
            candidates.push(exe_dir.join(&file_name));
        }
    }

    if let Ok(resource_dir) = app.path().resource_dir() {
        candidates.push(resource_dir.join(&file_name));
        candidates.push(resource_dir.join("binaries").join(&file_name));
    }

    for candidate in &candidates {
        if candidate.is_file() {
            return Ok(candidate.clone());
        }
    }

    Err(io::Error::other(format!(
        "go sidecar binary {} was not found; checked {}",
        file_name.to_string_lossy(),
        candidates
            .iter()
            .map(|candidate| candidate.display().to_string())
            .collect::<Vec<_>>()
            .join(", ")
    ))
    .into())
}

fn sidecar_binary_file_name() -> OsString {
    #[cfg(target_os = "windows")]
    {
        OsString::from(format!("{GO_SIDECAR_BINARY_NAME}.exe"))
    }

    #[cfg(not(target_os = "windows"))]
    {
        OsString::from(GO_SIDECAR_BINARY_NAME)
    }
}

fn pipe_child_output(child: &mut Child, stream_name: &str) {
    match stream_name {
        "stdout" => {
            if let Some(stdout) = child.stdout.take() {
                spawn_output_logger(stdout, stream_name);
            }
        }
        "stderr" => {
            if let Some(stderr) = child.stderr.take() {
                spawn_output_logger(stderr, stream_name);
            }
        }
        _ => {}
    }
}

fn spawn_output_logger<T>(stream: T, stream_name: &str)
where
    T: Read + Send + 'static,
{
    let label = stream_name.to_string();
    thread::spawn(move || {
        let reader = BufReader::new(stream);
        for line in reader.lines() {
            match line {
                Ok(line) if !line.trim().is_empty() => {
                    eprintln!("[xrayview] go sidecar {label}: {line}");
                }
                Ok(_) => {}
                Err(error) => {
                    eprintln!("[xrayview] go sidecar {label} stream error: {error}");
                    break;
                }
            }
        }
    });
}

fn wait_for_sidecar_ready(
    child: &mut Child,
    base_url: &GoSidecarBaseUrl,
) -> Result<(), Box<dyn Error>> {
    let deadline = Instant::now() + STARTUP_TIMEOUT;

    loop {
        if let Some(status) = child.try_wait()? {
            return Err(io::Error::other(format!(
                "go sidecar exited before becoming ready: {}",
                format_exit_status(status)
            ))
            .into());
        }

        match probe_sidecar(base_url, PROBE_TIMEOUT) {
            ProbeResult::ExpectedGoBackend => return Ok(()),
            ProbeResult::ReachableUnexpected => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(io::Error::other(format!(
                    "go sidecar startup failed because {} is serving an unexpected response",
                    base_url.raw
                ))
                .into());
            }
            ProbeResult::Unreachable => {}
        }

        if Instant::now() >= deadline {
            terminate_child(child);
            return Err(io::Error::other(format!(
                "go sidecar did not become ready within {}s at {}",
                STARTUP_TIMEOUT.as_secs(),
                base_url.raw
            ))
            .into());
        }

        thread::sleep(PROBE_INTERVAL);
    }
}

fn probe_sidecar(base_url: &GoSidecarBaseUrl, timeout: Duration) -> ProbeResult {
    let Ok(addresses) = (base_url.host.as_str(), base_url.port).to_socket_addrs() else {
        return ProbeResult::Unreachable;
    };

    for socket_address in addresses {
        let Ok(mut stream) = TcpStream::connect_timeout(&socket_address, timeout) else {
            continue;
        };
        let _ = stream.set_read_timeout(Some(timeout));
        let _ = stream.set_write_timeout(Some(timeout));

        let host_header = if base_url.host.contains(':') {
            format!("[{}]:{}", base_url.host, base_url.port)
        } else {
            format!("{}:{}", base_url.host, base_url.port)
        };
        let request = format!(
            "GET /healthz HTTP/1.1\r\nHost: {host_header}\r\nAccept: application/json\r\nConnection: close\r\n\r\n"
        );
        if stream.write_all(request.as_bytes()).is_err() {
            continue;
        }

        let mut response = String::new();
        if BufReader::new(stream)
            .read_to_string(&mut response)
            .is_err()
        {
            continue;
        }

        return classify_healthz_response(&response);
    }

    ProbeResult::Unreachable
}

fn classify_healthz_response(response: &str) -> ProbeResult {
    let Some((head, body)) = response.split_once("\r\n\r\n") else {
        return ProbeResult::ReachableUnexpected;
    };
    let Some(status_line) = head.lines().next() else {
        return ProbeResult::ReachableUnexpected;
    };
    let mut status_parts = status_line.split_whitespace();
    let _ = status_parts.next();
    let status_code = status_parts.next().unwrap_or_default();
    if status_code != "200" {
        return ProbeResult::ReachableUnexpected;
    }

    let Ok(payload) = serde_json::from_str::<Value>(body) else {
        return ProbeResult::ReachableUnexpected;
    };
    let service = payload.get("service").and_then(Value::as_str);
    let transport = payload.get("transport").and_then(Value::as_str);
    let status = payload.get("status").and_then(Value::as_str);
    if service == Some(EXPECTED_SERVICE_NAME)
        && transport == Some(EXPECTED_TRANSPORT_KIND)
        && status == Some("ok")
    {
        ProbeResult::ExpectedGoBackend
    } else {
        ProbeResult::ReachableUnexpected
    }
}

fn terminate_child(child: &mut Child) {
    if child.try_wait().ok().flatten().is_some() {
        return;
    }

    #[cfg(unix)]
    {
        let pid = child.id() as i32;
        if unsafe { libc::kill(pid, libc::SIGTERM) } == 0
            && wait_for_child_exit(child, SHUTDOWN_GRACE_PERIOD)
        {
            return;
        }
    }

    let _ = child.kill();
    let _ = child.wait();
}

fn wait_for_child_exit(child: &mut Child, timeout: Duration) -> bool {
    let deadline = Instant::now() + timeout;
    loop {
        match child.try_wait() {
            Ok(Some(_)) => return true,
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(50)),
            Ok(None) => return false,
            Err(_) => return false,
        }
    }
}

fn format_exit_status(status: ExitStatus) -> String {
    match status.code() {
        Some(code) => format!("exit code {code}"),
        None => "terminated by signal".to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fake_response(body: &str) -> String {
        format!(
            "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: {}\r\n\r\n{}",
            body.len(),
            body
        )
    }

    #[test]
    fn explicit_runtime_modes_are_detected() {
        assert_eq!(runtime_mode_from_raw("go-sidecar"), RuntimeMode::GoSidecar);
        assert_eq!(
            runtime_mode_from_raw("legacy-rust"),
            RuntimeMode::LegacyRust
        );
        assert_eq!(runtime_mode_from_raw("mock"), RuntimeMode::Mock);
    }

    #[test]
    fn go_sidecar_is_the_default_runtime_mode() {
        assert_eq!(runtime_mode_from_raw(""), RuntimeMode::GoSidecar);
        assert_eq!(runtime_mode_from_raw("unknown"), RuntimeMode::GoSidecar);
    }

    #[test]
    fn desktop_runtime_modes_that_require_sidecar_are_explicit() {
        assert!(RuntimeMode::LegacyRust.requires_go_sidecar());
        assert!(RuntimeMode::GoSidecar.requires_go_sidecar());
        assert!(!RuntimeMode::Mock.requires_go_sidecar());
    }

    #[test]
    fn invalid_sidecar_url_falls_back_to_default() {
        let (base_url, warning) =
            resolve_go_sidecar_base_url_from_raw(Some("http://example.com:38181".to_string()));

        assert_eq!(base_url, default_go_sidecar_base_url());
        assert!(warning.is_some());
    }

    #[test]
    fn parses_valid_loopback_sidecar_url() {
        let parsed = parse_go_sidecar_base_url("http://localhost:41234").unwrap();

        assert_eq!(
            parsed,
            GoSidecarBaseUrl {
                raw: "http://localhost:41234".to_string(),
                host: "localhost".to_string(),
                port: 41234,
            }
        );
    }

    #[test]
    fn classify_expected_healthz_response() {
        let response = fake_response(
            r#"{"status":"ok","service":"xrayview-go-backend","transport":"local-http-json"}"#,
        );

        assert_eq!(
            classify_healthz_response(&response),
            ProbeResult::ExpectedGoBackend
        );
    }

    #[test]
    fn classify_unexpected_healthz_response() {
        let response =
            fake_response(r#"{"status":"ok","service":"other","transport":"local-http-json"}"#);

        assert_eq!(
            classify_healthz_response(&response),
            ProbeResult::ReachableUnexpected
        );
    }
}
