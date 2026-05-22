use std::{
    collections::BTreeMap,
    fs,
    io::{self, BufRead, BufReader, Read, Write},
    net::{IpAddr, TcpListener, TcpStream},
    path::{Path, PathBuf},
    sync::{
        Arc,
        mpsc::{Receiver, RecvTimeoutError},
    },
    thread,
    time::{Duration, UNIX_EPOCH},
};

use chrono::SecondsFormat;
use serde::{Serialize, de::DeserializeOwned};
use url::Url;

use crate::{
    app::App,
    contracts::{self, BackendError, BackendErrorCode},
};

pub const TRANSPORT_KIND: &str = "local-http-json";
pub const API_BASE_PATH: &str = "/api/v1";
pub const RUNTIME_PATH: &str = "/api/v1/runtime";
pub const COMMANDS_PATH: &str = "/api/v1/commands";
pub const COMMAND_ENDPOINT_TEMPLATE: &str = "/api/v1/commands/{command}";
pub const EVENTS_PATH: &str = "/api/v1/events";
pub const PREVIEW_PATH: &str = "/preview";
const SSE_DISCONNECT_POLL_INTERVAL: Duration = Duration::from_millis(100);

#[derive(Debug, Clone)]
pub struct Request {
    pub method: String,
    pub target: String,
    pub headers: BTreeMap<String, String>,
    pub body: Vec<u8>,
}

impl Request {
    pub fn new(method: impl Into<String>, target: impl Into<String>) -> Self {
        Self {
            method: method.into(),
            target: target.into(),
            headers: BTreeMap::new(),
            body: Vec::new(),
        }
    }

    pub fn with_header(mut self, name: impl Into<String>, value: impl Into<String>) -> Self {
        self.headers.insert(name.into(), value.into());
        self
    }

    pub fn with_body(mut self, body: impl Into<Vec<u8>>) -> Self {
        self.body = body.into();
        self
    }

    fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(key, _)| key.eq_ignore_ascii_case(name))
            .map(|(_, value)| value.trim())
    }

    fn path_and_query(&self) -> (&str, Option<&str>) {
        self.target
            .split_once('?')
            .map_or((self.target.as_str(), None), |(path, query)| {
                (path, Some(query))
            })
    }
}

#[derive(Debug, Clone)]
pub struct Response {
    pub status: u16,
    pub headers: BTreeMap<String, String>,
    pub body: Vec<u8>,
}

impl Response {
    fn empty(status: u16) -> Self {
        Self {
            status,
            headers: BTreeMap::new(),
            body: Vec::new(),
        }
    }

    fn text(status: u16, body: impl Into<String>) -> Self {
        let mut response = Self::empty(status);
        response.headers.insert(
            "content-type".to_string(),
            "text/plain; charset=utf-8".to_string(),
        );
        response.body = body.into().into_bytes();
        response
    }

    fn json<T: Serialize>(status: u16, payload: &T) -> Self {
        let mut response = Self::empty(status);
        response.headers.insert(
            "content-type".to_string(),
            "application/json; charset=utf-8".to_string(),
        );
        response.body = serde_json::to_vec(payload).expect("failed to serialize response payload");
        response.body.push(b'\n');
        response
    }
}

#[derive(Clone)]
pub struct Router {
    app: Arc<App>,
}

impl Router {
    pub fn new(app: Arc<App>) -> Self {
        Self { app }
    }

    pub fn handle(&self, request: Request) -> Response {
        let origin = request
            .header("Origin")
            .filter(|origin| !origin.is_empty())
            .map(str::to_string);
        if let Some(origin) = origin.as_deref() {
            if !is_allowed_origin(origin) {
                return Response::json(
                    403,
                    &BackendError::invalid_input(
                        "request origin is not allowed for the local backend transport",
                    )
                    .with_details([origin.to_string()]),
                );
            }
        }

        if request.method == "OPTIONS" {
            let mut response = Response::empty(204);
            if let Some(origin) = origin.as_deref() {
                apply_cors_headers(&mut response.headers, origin);
            }
            return response;
        }

        let mut response = self.dispatch(request);
        if let Some(origin) = origin.as_deref() {
            apply_cors_headers(&mut response.headers, origin);
        }
        response
    }

    fn dispatch(&self, request: Request) -> Response {
        let (path, query) = request.path_and_query();
        match (request.method.as_str(), path) {
            ("GET", "/healthz") | ("GET", RUNTIME_PATH) => {
                Response::json(200, &self.runtime_response())
            }
            ("GET", COMMANDS_PATH) => Response::json(
                200,
                &CommandListResponse {
                    commands: contracts::SUPPORTED_COMMANDS
                        .iter()
                        .map(|command| (*command).to_string())
                        .collect(),
                    backend_contract_version: contracts::BACKEND_CONTRACT_VERSION,
                },
            ),
            ("GET", PREVIEW_PATH) => self.handle_preview(query, request.header("If-None-Match")),
            ("GET", EVENTS_PATH) => Response::text(500, "streaming not supported\n"),
            (_, path) if path.starts_with(&format!("{COMMANDS_PATH}/")) => {
                self.handle_command(&request)
            }
            ("GET", "/") => Response::json(
                200,
                &serde_json::json!({
                    "service": contracts::SERVICE_NAME,
                    "status": "ok",
                }),
            ),
            _ => Response::text(404, "404 page not found\n"),
        }
    }

    fn handle_command(&self, request: &Request) -> Response {
        if request.method != "POST" {
            return self.backend_error(
                405,
                BackendError::invalid_input("commands must be called with POST")
                    .with_details([request.method.clone()]),
            );
        }

        let (path, _) = request.path_and_query();
        let command_name = path.trim_start_matches(&format!("{COMMANDS_PATH}/"));
        if command_name.is_empty() || command_name.contains('/') {
            return self.backend_error(404, BackendError::not_found("command not found"));
        }

        if !contracts::is_supported_command(command_name) {
            return self.backend_error(
                404,
                BackendError::not_found(format!("unsupported command: {command_name}"))
                    .with_details(contracts::SUPPORTED_COMMANDS),
            );
        }

        match command_name {
            contracts::COMMAND_GET_PROCESSING_MANIFEST => {
                Response::json(200, &self.app.get_processing_manifest())
            }
            contracts::COMMAND_OPEN_STUDY => {
                self.decode_and_run(request, |command| self.app.open_study(command))
            }
            contracts::COMMAND_START_RENDER_JOB => {
                self.decode_and_run(request, |command| self.app.start_render_job_async(command))
            }
            contracts::COMMAND_START_ANALYZE_JOB => {
                self.decode_and_run(request, |command| self.app.start_analyze_job_async(command))
            }
            contracts::COMMAND_START_PROCESS_JOB => {
                self.decode_and_run(request, |command| self.app.start_process_job_async(command))
            }
            contracts::COMMAND_GET_JOB => {
                self.decode_and_run(request, |command| self.app.get_job(command))
            }
            contracts::COMMAND_GET_JOBS => {
                self.decode_and_run(request, |command| self.app.get_jobs(command))
            }
            contracts::COMMAND_CANCEL_JOB => {
                self.decode_and_run(request, |command| self.app.cancel_job(command))
            }
            contracts::COMMAND_MEASURE_LINE_ANNOTATION => {
                self.decode_and_run(request, |command| self.app.measure_line_annotation(command))
            }
            _ => self.backend_error(
                501,
                BackendError::internal(format!(
                    "command {command_name} is not implemented in the Rust backend yet"
                ))
                .with_details([format!("transport={TRANSPORT_KIND}")]),
            ),
        }
    }

    fn decode_and_run<Command, Output, F>(&self, request: &Request, handler: F) -> Response
    where
        Command: DeserializeOwned,
        Output: Serialize,
        F: FnOnce(Command) -> Result<Output, BackendError>,
    {
        let command = match decode_json_request::<Command>(&request.body) {
            Ok(command) => command,
            Err(error) => return self.backend_error(status_code_for_backend_error(&error), error),
        };

        match handler(command) {
            Ok(result) => Response::json(200, &result),
            Err(error) => self.backend_error(status_code_for_backend_error(&error), error),
        }
    }

    fn handle_preview(&self, query: Option<&str>, if_none_match: Option<&str>) -> Response {
        let Some(raw_path) = query_parameter(query, "path") else {
            return Response::text(400, "preview path is required\n");
        };
        let raw_path = raw_path.trim();
        if raw_path.is_empty() {
            return Response::text(400, "preview path is required\n");
        }

        let requested_path = PathBuf::from(raw_path);
        if !requested_path.is_absolute() {
            return Response::text(400, "preview path must be absolute\n");
        }

        let cache_root = &self.app.config().paths.cache_dir;
        let resolved_root = match fs::canonicalize(cache_root) {
            Ok(path) => path,
            Err(error) => {
                return Response::text(500, format!("cache root unavailable: {error}\n"));
            }
        };
        let resolved_target = match fs::canonicalize(&requested_path) {
            Ok(path) => path,
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                return Response::text(
                    404,
                    format!("preview artifact not found: {}\n", requested_path.display()),
                );
            }
            Err(error) => return Response::text(500, format!("{error}\n")),
        };

        if !path_is_inside(&resolved_root, &resolved_target) {
            return Response::text(403, "preview path outside cache root\n");
        }

        let metadata = match fs::metadata(&resolved_target) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                return Response::text(
                    404,
                    format!("preview artifact not found: {}\n", requested_path.display()),
                );
            }
            Err(error) => return Response::text(500, format!("{error}\n")),
        };
        if metadata.is_dir() {
            return Response::text(403, "preview path is a directory\n");
        }

        let etag = preview_etag(&metadata);
        if if_none_match.is_some_and(|value| value == etag) {
            let mut response = Response::empty(304);
            response
                .headers
                .insert("etag".to_string(), etag);
            response
                .headers
                .insert("cache-control".to_string(), "public, max-age=3600".to_string());
            return response;
        }

        match fs::read(&resolved_target) {
            Ok(body) => {
                let mut response = Response::empty(200);
                response.headers.insert(
                    "content-type".to_string(),
                    content_type_for_path(&resolved_target).to_string(),
                );
                response
                    .headers
                    .insert("etag".to_string(), etag);
                response.headers.insert(
                    "cache-control".to_string(),
                    "public, max-age=3600".to_string(),
                );
                response.body = body;
                response
            }
            Err(error) => Response::text(500, format!("{error}\n")),
        }
    }

    fn runtime_response(&self) -> RuntimeResponse {
        RuntimeResponse {
            status: "ok".to_string(),
            service: self.app.config().service_name.clone(),
            transport: TRANSPORT_KIND.to_string(),
            local_only: true,
            backend_contract_version: contracts::BACKEND_CONTRACT_VERSION,
            backend_contract_schema: contracts::BACKEND_CONTRACT_SCHEMA_ID.to_string(),
            api_base_path: API_BASE_PATH.to_string(),
            command_endpoint: COMMAND_ENDPOINT_TEMPLATE.to_string(),
            listen_address: self.app.config().listen_address(),
            cache_dir: self.app.config().paths.cache_dir.display().to_string(),
            persistence_dir: self
                .app
                .config()
                .paths
                .persistence_dir
                .display()
                .to_string(),
            supported_commands: contracts::SUPPORTED_COMMANDS
                .iter()
                .map(|command| (*command).to_string())
                .collect(),
            supported_job_kinds: self
                .app
                .supported_job_kinds()
                .iter()
                .map(|kind| (*kind).to_string())
                .collect(),
            study_count: self.app.study_count(),
            started_at: self
                .app
                .started_at()
                .to_rfc3339_opts(SecondsFormat::Secs, true),
        }
    }

    fn backend_error(&self, status: u16, error: BackendError) -> Response {
        Response::json(status, &error)
    }

    fn is_event_stream_request(&self, request: &Request) -> bool {
        let (path, _) = request.path_and_query();
        request.method == "GET" && path == EVENTS_PATH
    }

    fn handle_event_stream(&self, stream: &mut TcpStream, request: Request) -> io::Result<()> {
        let origin = request
            .header("Origin")
            .filter(|origin| !origin.is_empty())
            .map(str::to_string);
        if let Some(origin) = origin.as_deref() {
            if !is_allowed_origin(origin) {
                return write_http_response(
                    stream,
                    Response::json(
                        403,
                        &BackendError::invalid_input(
                            "request origin is not allowed for the local backend transport",
                        )
                        .with_details([origin.to_string()]),
                    ),
                );
            }
        }

        let subscription = self.app.subscribe_job_updates();
        let mut headers = BTreeMap::new();
        headers.insert("content-type".to_string(), "text/event-stream".to_string());
        headers.insert("cache-control".to_string(), "no-cache".to_string());
        headers.insert("connection".to_string(), "keep-alive".to_string());
        headers.insert("x-accel-buffering".to_string(), "no".to_string());
        if let Some(origin) = origin.as_deref() {
            apply_cors_headers(&mut headers, origin);
        }
        write_http_headers(stream, 200, headers)?;
        // Hint browsers to reconnect quickly after a transient drop. The polling fallback in
        // useJobs.ts still kicks in after EVENT_STALE_MS, but a 1s retry keeps the gap small.
        stream.write_all(b"retry: 1000\n\n")?;
        stream.flush()?;

        let result = stream_job_updates_tcp(stream, subscription.receiver);
        self.app.unsubscribe_job_updates(subscription.id);
        result
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct RuntimeResponse {
    status: String,
    service: String,
    transport: String,
    local_only: bool,
    backend_contract_version: u32,
    #[serde(rename = "backendContractSchemaId")]
    backend_contract_schema: String,
    api_base_path: String,
    command_endpoint: String,
    listen_address: String,
    cache_dir: String,
    persistence_dir: String,
    supported_commands: Vec<String>,
    supported_job_kinds: Vec<String>,
    study_count: usize,
    started_at: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct CommandListResponse {
    commands: Vec<String>,
    backend_contract_version: u32,
}

pub fn serve(app: Arc<App>) -> io::Result<()> {
    let listener = TcpListener::bind(app.config().listen_address())?;
    let router = Router::new(app);

    for stream in listener.incoming() {
        let stream = stream?;
        let router = router.clone();
        thread::spawn(move || {
            let _ = handle_connection(stream, router);
        });
    }

    Ok(())
}

fn handle_connection(mut stream: TcpStream, router: Router) -> io::Result<()> {
    let request = match parse_http_request(&stream)? {
        Some(request) => request,
        None => return Ok(()),
    };
    if router.is_event_stream_request(&request) {
        return router.handle_event_stream(&mut stream, request);
    }
    let response = router.handle(request);
    write_http_response(&mut stream, response)
}

fn parse_http_request(stream: &TcpStream) -> io::Result<Option<Request>> {
    let mut reader = BufReader::new(stream);
    let mut request_line = String::new();
    if reader.read_line(&mut request_line)? == 0 {
        return Ok(None);
    }
    let mut parts = request_line.split_whitespace();
    let Some(method) = parts.next() else {
        return Ok(None);
    };
    let Some(target) = parts.next() else {
        return Ok(None);
    };

    let mut headers = BTreeMap::new();
    loop {
        let mut line = String::new();
        reader.read_line(&mut line)?;
        let trimmed = line.trim_end_matches(['\r', '\n']);
        if trimmed.is_empty() {
            break;
        }
        if let Some((name, value)) = trimmed.split_once(':') {
            headers.insert(name.trim().to_string(), value.trim().to_string());
        }
    }

    let content_length = headers
        .iter()
        .find(|(name, _)| name.eq_ignore_ascii_case("content-length"))
        .and_then(|(_, value)| value.parse::<usize>().ok())
        .unwrap_or(0);
    let mut body = vec![0; content_length];
    if content_length > 0 {
        reader.read_exact(&mut body)?;
    }

    Ok(Some(Request {
        method: method.to_string(),
        target: target.to_string(),
        headers,
        body,
    }))
}

fn write_http_response(stream: &mut TcpStream, mut response: Response) -> io::Result<()> {
    response
        .headers
        .entry("content-length".to_string())
        .or_insert_with(|| response.body.len().to_string());
    response
        .headers
        .entry("connection".to_string())
        .or_insert_with(|| "close".to_string());

    write!(
        stream,
        "HTTP/1.1 {} {}\r\n",
        response.status,
        reason_phrase(response.status)
    )?;
    for (name, value) in response.headers {
        write!(stream, "{name}: {value}\r\n")?;
    }
    stream.write_all(b"\r\n")?;
    stream.write_all(&response.body)
}

fn write_http_headers(
    stream: &mut TcpStream,
    status: u16,
    headers: BTreeMap<String, String>,
) -> io::Result<()> {
    write!(stream, "HTTP/1.1 {} {}\r\n", status, reason_phrase(status))?;
    for (name, value) in headers {
        write!(stream, "{name}: {value}\r\n")?;
    }
    stream.write_all(b"\r\n")
}

fn stream_job_updates_tcp(
    stream: &mut TcpStream,
    receiver: Receiver<contracts::JobSnapshot>,
) -> io::Result<()> {
    stream.set_read_timeout(Some(SSE_DISCONNECT_POLL_INTERVAL))?;
    loop {
        match receiver.recv_timeout(SSE_DISCONNECT_POLL_INTERVAL) {
            Ok(snapshot) => {
                let frame = sse_frame(&snapshot)?;
                stream.write_all(&frame)?;
                stream.flush()?;
            }
            Err(RecvTimeoutError::Timeout) => {
                if !tcp_client_is_connected(stream)? {
                    return Ok(());
                }
            }
            Err(RecvTimeoutError::Disconnected) => return Ok(()),
        }
    }
}

fn tcp_client_is_connected(stream: &TcpStream) -> io::Result<bool> {
    let mut byte = [0_u8; 1];
    match stream.peek(&mut byte) {
        Ok(0) => Ok(false),
        Ok(_) => Ok(true),
        Err(error)
            if matches!(
                error.kind(),
                io::ErrorKind::WouldBlock | io::ErrorKind::TimedOut
            ) =>
        {
            Ok(true)
        }
        Err(error) => Err(error),
    }
}

#[cfg(test)]
fn stream_job_updates<W: Write>(
    stream: &mut W,
    receiver: Receiver<contracts::JobSnapshot>,
) -> io::Result<()> {
    while let Ok(snapshot) = receiver.recv() {
        let frame = sse_frame(&snapshot)?;
        stream.write_all(&frame)?;
        stream.flush()?;
    }
    Ok(())
}

fn sse_frame(snapshot: &contracts::JobSnapshot) -> io::Result<Vec<u8>> {
    let mut frame = Vec::new();
    frame.extend_from_slice(b"data: ");
    serde_json::to_writer(&mut frame, snapshot)
        .map_err(|error| io::Error::other(format!("serialize SSE job update: {error}")))?;
    frame.extend_from_slice(b"\n\n");
    Ok(frame)
}

fn decode_json_request<T: DeserializeOwned>(body: &[u8]) -> Result<T, BackendError> {
    let mut deserializer = serde_json::Deserializer::from_slice(body);
    let payload = T::deserialize(&mut deserializer).map_err(|error| {
        BackendError::invalid_input("invalid command payload").with_details([error.to_string()])
    })?;
    deserializer.end().map_err(|_| {
        BackendError::invalid_input("invalid command payload")
            .with_details(["unexpected trailing JSON content"])
    })?;
    Ok(payload)
}

fn status_code_for_backend_error(error: &BackendError) -> u16 {
    match error.code {
        BackendErrorCode::InvalidInput => 400,
        BackendErrorCode::NotFound => 404,
        BackendErrorCode::Conflict | BackendErrorCode::Cancelled => 409,
        BackendErrorCode::CacheCorrupted | BackendErrorCode::Internal => 500,
    }
}

fn is_allowed_origin(origin: &str) -> bool {
    let Ok(parsed) = Url::parse(origin) else {
        return false;
    };

    if parsed.scheme() != "http" && parsed.scheme() != "https" {
        return false;
    }

    let Some(host) = parsed.host_str() else {
        return false;
    };
    if host == "localhost" || host.ends_with(".localhost") {
        return true;
    }

    host.parse::<IpAddr>().is_ok_and(|ip| ip.is_loopback())
}

fn apply_cors_headers(headers: &mut BTreeMap<String, String>, origin: &str) {
    append_vary(headers, "Origin");
    append_vary(headers, "Access-Control-Request-Method");
    append_vary(headers, "Access-Control-Request-Headers");
    headers.insert(
        "access-control-allow-origin".to_string(),
        origin.to_string(),
    );
    headers.insert(
        "access-control-allow-methods".to_string(),
        "GET, POST, OPTIONS".to_string(),
    );
    headers.insert(
        "access-control-allow-headers".to_string(),
        "Accept, Content-Type".to_string(),
    );
    headers.insert("access-control-max-age".to_string(), "600".to_string());
}

fn append_vary(headers: &mut BTreeMap<String, String>, value: &str) {
    let Some(existing) = headers.get_mut("vary") else {
        headers.insert("vary".to_string(), value.to_string());
        return;
    };
    if existing
        .split(',')
        .any(|part| part.trim().eq_ignore_ascii_case(value))
    {
        return;
    }
    existing.push_str(", ");
    existing.push_str(value);
}

fn query_parameter(query: Option<&str>, key: &str) -> Option<String> {
    query.and_then(|query| {
        url::form_urlencoded::parse(query.as_bytes())
            .find(|(name, _)| name == key)
            .map(|(_, value)| value.into_owned())
    })
}

fn path_is_inside(root: &Path, target: &Path) -> bool {
    target == root || target.starts_with(root)
}

fn preview_etag(metadata: &fs::Metadata) -> String {
    let modified_nanos = metadata
        .modified()
        .ok()
        .and_then(|time| time.duration_since(UNIX_EPOCH).ok())
        .map(|duration| duration.as_nanos())
        .unwrap_or(0);
    format!("\"{modified_nanos:x}-{:x}\"", metadata.len())
}

fn content_type_for_path(path: &Path) -> &'static str {
    match path.extension().and_then(|extension| extension.to_str()) {
        Some(extension) if extension.eq_ignore_ascii_case("png") => "image/png",
        Some(extension) if extension.eq_ignore_ascii_case("jpg") => "image/jpeg",
        Some(extension) if extension.eq_ignore_ascii_case("jpeg") => "image/jpeg",
        Some(extension) if extension.eq_ignore_ascii_case("bmp") => "image/bmp",
        _ => "application/octet-stream",
    }
}

fn reason_phrase(status: u16) -> &'static str {
    match status {
        200 => "OK",
        204 => "No Content",
        304 => "Not Modified",
        400 => "Bad Request",
        403 => "Forbidden",
        404 => "Not Found",
        405 => "Method Not Allowed",
        409 => "Conflict",
        500 => "Internal Server Error",
        501 => "Not Implemented",
        _ => "OK",
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        config::Config,
        contracts::{AnnotationPoint, AnnotationSource, LineAnnotation, RenderStudyCommand},
    };

    fn test_router() -> Router {
        Router::new(Arc::new(App::new(Config::default()).unwrap()))
    }

    fn wait_for_terminal_http_job(router: &Router, job_id: &str) -> serde_json::Value {
        for _ in 0..1000 {
            let response = router.handle(
                Request::new("POST", format!("{COMMANDS_PATH}/get_job")).with_body(
                    serde_json::to_vec(&serde_json::json!({ "jobId": job_id })).unwrap(),
                ),
            );
            assert_eq!(response.status, 200);
            let snapshot: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
            if matches!(
                snapshot["state"].as_str(),
                Some("completed" | "failed" | "cancelled")
            ) {
                return snapshot;
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        panic!("job {job_id} did not reach a terminal state");
    }

    #[test]
    fn healthz_includes_contract_metadata() {
        let response = test_router().handle(Request::new("GET", "/healthz"));

        assert_eq!(response.status, 200);
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
        assert_eq!(payload["service"], contracts::SERVICE_NAME);
        assert_eq!(payload["transport"], TRANSPORT_KIND);
        assert_eq!(payload["localOnly"], true);
        assert_eq!(
            payload["backendContractVersion"],
            contracts::BACKEND_CONTRACT_VERSION
        );
        assert_eq!(payload["apiBasePath"], API_BASE_PATH);
        assert_eq!(payload["commandEndpoint"], COMMAND_ENDPOINT_TEMPLATE);
    }

    #[test]
    fn commands_endpoint_lists_supported_commands() {
        let response = test_router().handle(Request::new("GET", COMMANDS_PATH));
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
        let commands = payload["commands"].as_array().unwrap();

        assert_eq!(response.status, 200);
        assert_eq!(commands.len(), contracts::SUPPORTED_COMMANDS.len());
        assert_eq!(commands[0], contracts::COMMAND_GET_PROCESSING_MANIFEST);
    }

    #[test]
    fn events_endpoint_is_registered_for_streaming_connections() {
        let response = test_router().handle(Request::new("GET", EVENTS_PATH));

        assert_eq!(response.status, 500);
        assert_eq!(response.body, b"streaming not supported\n");
    }

    #[test]
    fn sse_frame_serializes_job_snapshot_as_data_frame() {
        let snapshot = contracts::JobSnapshot {
            job_id: "job-1".to_string(),
            job_kind: contracts::JobKind::RenderStudy,
            study_id: Some("study-1".to_string()),
            state: contracts::JobState::Completed,
            progress: contracts::JobProgress {
                percent: 100,
                stage: "completed".to_string(),
                message: "Completed".to_string(),
            },
            from_cache: false,
            result: None,
            error: None,
        };

        let frame = sse_frame(&snapshot).unwrap();
        let frame_text = String::from_utf8(frame).unwrap();

        assert!(frame_text.starts_with("data: "));
        assert!(frame_text.ends_with("\n\n"));
        let payload = frame_text
            .strip_prefix("data: ")
            .unwrap()
            .trim_end_matches('\n');
        let decoded: contracts::JobSnapshot = serde_json::from_str(payload).unwrap();
        assert_eq!(decoded, snapshot);
    }

    #[test]
    fn stream_job_updates_writes_sse_frames_until_sender_closes() {
        let snapshot = contracts::JobSnapshot {
            job_id: "job-1".to_string(),
            job_kind: contracts::JobKind::RenderStudy,
            study_id: Some("study-1".to_string()),
            state: contracts::JobState::Completed,
            progress: contracts::JobProgress {
                percent: 100,
                stage: "completed".to_string(),
                message: "Completed".to_string(),
            },
            from_cache: false,
            result: None,
            error: None,
        };
        let (sender, receiver) = std::sync::mpsc::sync_channel(1);
        sender.send(snapshot.clone()).unwrap();
        drop(sender);
        let mut stream = Vec::new();

        stream_job_updates(&mut stream, receiver).unwrap();

        let frame = String::from_utf8(stream).unwrap();
        let payload = frame.strip_prefix("data: ").unwrap().trim_end_matches('\n');
        let decoded: contracts::JobSnapshot = serde_json::from_str(payload).unwrap();
        assert_eq!(decoded, snapshot);
    }

    #[test]
    fn tcp_events_stream_delivers_job_updates_and_exits_after_disconnect() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-http-sse-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.dcm");
        fs::write(
            &input_path,
            crate::dicom::tests::build_renderable_test_dicom(Some("0.20\\0.30")),
        )
        .unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let app = Arc::new(App::new(config).unwrap());
        app.prepare().unwrap();
        let router = Router::new(Arc::clone(&app));
        let listener = match TcpListener::bind("127.0.0.1:0") {
            Ok(listener) => listener,
            Err(error) if error.kind() == io::ErrorKind::PermissionDenied => {
                eprintln!("skipping TCP SSE test because local bind is denied by the sandbox");
                return;
            }
            Err(error) => panic!("bind TCP SSE test listener: {error}"),
        };
        let address = listener.local_addr().unwrap();
        let (done_sender, done_receiver) = std::sync::mpsc::channel();
        thread::spawn(move || {
            let (stream, _) = listener.accept().unwrap();
            let result = handle_connection(stream, router).map_err(|error| error.kind());
            done_sender.send(result).unwrap();
        });

        let mut client = TcpStream::connect(address).unwrap();
        client
            .write_all(format!("GET {EVENTS_PATH} HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n").as_bytes())
            .unwrap();
        client
            .set_read_timeout(Some(Duration::from_secs(2)))
            .unwrap();
        let mut reader = BufReader::new(client.try_clone().unwrap());

        let mut status_line = String::new();
        reader.read_line(&mut status_line).unwrap();
        assert!(status_line.starts_with("HTTP/1.1 200 OK"));
        let mut content_type_seen = false;
        loop {
            let mut line = String::new();
            reader.read_line(&mut line).unwrap();
            if line == "\r\n" {
                break;
            }
            if line.eq_ignore_ascii_case("content-type: text/event-stream\r\n") {
                content_type_seen = true;
            }
        }
        assert!(content_type_seen);

        // The stream begins with `retry: 1000\n\n` as a reconnect hint before any data frames.
        let mut retry_line = String::new();
        reader.read_line(&mut retry_line).unwrap();
        assert_eq!(retry_line, "retry: 1000\n");
        let mut blank_line = String::new();
        reader.read_line(&mut blank_line).unwrap();
        assert_eq!(blank_line, "\n");

        let study = app
            .register_study(input_path.display().to_string(), None)
            .unwrap();
        app.start_render_job(RenderStudyCommand {
            study_id: study.study_id,
        })
        .unwrap();

        let mut event_line = String::new();
        reader.read_line(&mut event_line).unwrap();
        let payload = event_line
            .strip_prefix("data: ")
            .expect("SSE frame should be a data frame")
            .trim_end();
        let snapshot: contracts::JobSnapshot = serde_json::from_str(payload).unwrap();
        assert_eq!(snapshot.state, contracts::JobState::Completed);
        assert_eq!(snapshot.job_kind, contracts::JobKind::RenderStudy);

        drop(reader);
        drop(client);
        let result = done_receiver
            .recv_timeout(Duration::from_secs(2))
            .expect("SSE handler should return after client disconnect");
        assert_eq!(result, Ok(()));

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn get_processing_manifest_returns_frozen_payload() {
        let response = test_router().handle(Request::new(
            "POST",
            format!("{COMMANDS_PATH}/get_processing_manifest"),
        ));
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();

        assert_eq!(response.status, 200);
        assert_eq!(payload["defaultPresetId"], "default");
        assert_eq!(payload["presets"].as_array().unwrap().len(), 3);
        assert_eq!(payload["presets"][1]["id"], "xray");
        assert_eq!(payload["presets"][1]["controls"]["palette"], "bone");
    }

    #[test]
    fn command_endpoint_rejects_trailing_json_content() {
        let response = test_router().handle(
            Request::new("POST", format!("{COMMANDS_PATH}/open_study"))
                .with_body(br#"{"inputPath":"/tmp/missing.dcm"} true"#.to_vec()),
        );
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();

        assert_eq!(response.status, 400);
        assert_eq!(payload["code"], "invalidInput");
        assert_eq!(payload["details"][0], "unexpected trailing JSON content");
    }

    #[test]
    fn measure_line_annotation_returns_measured_pixels() {
        let app = Arc::new(App::new(Config::default()).unwrap());
        let study = app
            .register_study("/tmp/manual-measurement.dcm", None)
            .unwrap();
        let router = Router::new(app);
        let body = serde_json::json!({
            "studyId": study.study_id,
            "annotation": {
                "id": "line-1",
                "label": "Measurement 1",
                "source": "manual",
                "start": { "x": 12.0, "y": 18.0 },
                "end": { "x": 15.0, "y": 22.0 },
                "editable": true
            }
        });

        let response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/measure_line_annotation"))
                .with_body(serde_json::to_vec(&body).unwrap()),
        );
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();

        assert_eq!(response.status, 200);
        assert_eq!(payload["annotation"]["measurement"]["pixelLength"], 5.0);
        assert!(payload["annotation"]["measurement"]["calibratedLengthMm"].is_null());
    }

    #[test]
    fn measure_line_annotation_rejects_unknown_study() {
        let body = serde_json::json!({
            "studyId": "missing-study",
            "annotation": {
                "id": "line-1",
                "label": "Measurement 1",
                "source": "manual",
                "start": { "x": 0.0, "y": 0.0 },
                "end": { "x": 1.0, "y": 1.0 },
                "editable": true
            }
        });

        let response = test_router().handle(
            Request::new("POST", format!("{COMMANDS_PATH}/measure_line_annotation"))
                .with_body(serde_json::to_vec(&body).unwrap()),
        );
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();

        assert_eq!(response.status, 404);
        assert_eq!(payload["code"], "notFound");
    }

    #[test]
    fn render_job_commands_complete_preview_over_http() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-http-render-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.dcm");
        fs::write(
            &input_path,
            crate::dicom::tests::build_renderable_test_dicom(Some("0.20\\0.30")),
        )
        .unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let router = Router::new(Arc::new(App::new(config).unwrap()));

        let open_response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/open_study")).with_body(
                serde_json::to_vec(&serde_json::json!({
                    "inputPath": input_path.display().to_string(),
                }))
                .unwrap(),
            ),
        );
        assert_eq!(open_response.status, 200);
        let opened: serde_json::Value = serde_json::from_slice(&open_response.body).unwrap();
        let study_id = opened["study"]["studyId"].as_str().unwrap();

        let start_response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/start_render_job")).with_body(
                serde_json::to_vec(&serde_json::json!({ "studyId": study_id })).unwrap(),
            ),
        );
        assert_eq!(start_response.status, 200);
        let started: serde_json::Value = serde_json::from_slice(&start_response.body).unwrap();
        let job_id = started["jobId"].as_str().unwrap();

        let snapshot = wait_for_terminal_http_job(&router, job_id);
        assert_eq!(snapshot["state"], "completed");
        assert_eq!(snapshot["result"]["kind"], "renderStudy");
        assert_eq!(snapshot["result"]["payload"]["loadedWidth"], 4);
        assert_eq!(snapshot["result"]["payload"]["loadedHeight"], 2);
        assert!(
            fs::metadata(
                snapshot["result"]["payload"]["previewPath"]
                    .as_str()
                    .unwrap()
            )
            .unwrap()
            .is_file()
        );

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn process_job_commands_complete_preview_and_dicom_over_http() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-http-process-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.dcm");
        fs::write(
            &input_path,
            crate::dicom::tests::build_renderable_test_dicom(Some("0.20\\0.30")),
        )
        .unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let router = Router::new(Arc::new(App::new(config).unwrap()));

        let open_response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/open_study")).with_body(
                serde_json::to_vec(&serde_json::json!({
                    "inputPath": input_path.display().to_string(),
                }))
                .unwrap(),
            ),
        );
        assert_eq!(open_response.status, 200);
        let opened: serde_json::Value = serde_json::from_slice(&open_response.body).unwrap();
        let study_id = opened["study"]["studyId"].as_str().unwrap();

        let start_response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/start_process_job")).with_body(
                serde_json::to_vec(&serde_json::json!({
                    "studyId": study_id,
                    "presetId": "default",
                    "invert": true,
                    "brightness": 10,
                    "contrast": 1.0,
                    "equalize": false,
                    "compare": false
                }))
                .unwrap(),
            ),
        );
        assert_eq!(start_response.status, 200);
        let started: serde_json::Value = serde_json::from_slice(&start_response.body).unwrap();
        let job_id = started["jobId"].as_str().unwrap();

        let snapshot = wait_for_terminal_http_job(&router, job_id);
        assert_eq!(snapshot["state"], "completed");
        assert_eq!(snapshot["result"]["kind"], "processStudy");
        assert_eq!(
            snapshot["result"]["payload"]["mode"],
            "inverted grayscale with brightness +10"
        );
        assert!(
            fs::metadata(
                snapshot["result"]["payload"]["previewPath"]
                    .as_str()
                    .unwrap()
            )
            .unwrap()
            .is_file()
        );
        assert!(
            fs::metadata(snapshot["result"]["payload"]["dicomPath"].as_str().unwrap())
                .unwrap()
                .is_file()
        );

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn analyze_job_commands_complete_overlay_previews_over_http() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-http-analyze-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.dcm");
        fs::write(
            &input_path,
            crate::dicom::tests::build_renderable_test_dicom_with_pixels(
                20,
                20,
                Some("0.20\\0.30"),
                analyze_fixture_pixels(),
            ),
        )
        .unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let router = Router::new(Arc::new(App::new(config).unwrap()));

        let open_response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/open_study")).with_body(
                serde_json::to_vec(&serde_json::json!({
                    "inputPath": input_path.display().to_string(),
                }))
                .unwrap(),
            ),
        );
        assert_eq!(open_response.status, 200);
        let opened: serde_json::Value = serde_json::from_slice(&open_response.body).unwrap();
        let study_id = opened["study"]["studyId"].as_str().unwrap();

        let start_response = router.handle(
            Request::new("POST", format!("{COMMANDS_PATH}/start_analyze_job")).with_body(
                serde_json::to_vec(&serde_json::json!({ "studyId": study_id })).unwrap(),
            ),
        );
        assert_eq!(start_response.status, 200);
        let started: serde_json::Value = serde_json::from_slice(&start_response.body).unwrap();
        let job_id = started["jobId"].as_str().unwrap();

        let snapshot = wait_for_terminal_http_job(&router, job_id);
        assert_eq!(snapshot["state"], "completed");
        assert_eq!(snapshot["result"]["kind"], "analyzeStudy");
        assert_eq!(snapshot["result"]["payload"]["loadedWidth"], 20);
        assert_eq!(snapshot["result"]["payload"]["loadedHeight"], 20);
        assert!(
            fs::metadata(
                snapshot["result"]["payload"]["previewPath"]
                    .as_str()
                    .unwrap()
            )
            .unwrap()
            .is_file()
        );
        assert!(
            fs::metadata(
                snapshot["result"]["payload"]["filledPreviewPath"]
                    .as_str()
                    .unwrap()
            )
            .unwrap()
            .is_file()
        );

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    fn analyze_fixture_pixels() -> &'static [u8] {
        static PIXELS: [u8; 400] = {
            let mut pixels = [24_u8; 400];
            let mut y = 6;
            while y < 14 {
                let mut x = 7;
                while x < 13 {
                    pixels[y * 20 + x] = 210;
                    x += 1;
                }
                y += 1;
            }
            let mut x = 0;
            while x < 20 {
                pixels[10 * 20 + x] = if x % 2 == 0 { 40 } else { 180 };
                x += 1;
            }
            pixels
        };
        &PIXELS
    }

    #[test]
    fn allowed_origin_receives_cors_headers() {
        let response = test_router()
            .handle(Request::new("GET", "/healthz").with_header("Origin", "http://localhost:1420"));

        assert_eq!(response.status, 200);
        assert_eq!(
            response.headers.get("access-control-allow-origin"),
            Some(&"http://localhost:1420".to_string())
        );
    }

    #[test]
    fn options_preflight_returns_no_content() {
        let response = test_router().handle(
            Request::new("OPTIONS", format!("{COMMANDS_PATH}/open_study"))
                .with_header("Origin", "http://localhost:1420")
                .with_header("Access-Control-Request-Method", "POST")
                .with_header("Access-Control-Request-Headers", "content-type"),
        );

        assert_eq!(response.status, 204);
        assert_eq!(
            response.headers.get("access-control-allow-origin"),
            Some(&"http://localhost:1420".to_string())
        );
        assert!(
            response
                .headers
                .get("access-control-allow-methods")
                .unwrap()
                .contains("POST")
        );
    }

    #[test]
    fn disallowed_origin_is_rejected() {
        let response = test_router()
            .handle(Request::new("GET", "/healthz").with_header("Origin", "https://example.com"));
        let payload: serde_json::Value = serde_json::from_slice(&response.body).unwrap();

        assert_eq!(response.status, 403);
        assert_eq!(payload["code"], "invalidInput");
    }

    #[test]
    fn localhost_subdomain_origin_is_accepted() {
        let response = test_router()
            .handle(Request::new("GET", "/healthz").with_header("Origin", "http://tauri.localhost"));

        assert_eq!(response.status, 200);
        assert_eq!(
            response.headers.get("access-control-allow-origin"),
            Some(&"http://tauri.localhost".to_string())
        );
    }

    #[test]
    fn localhost_lookalike_subdomain_is_rejected() {
        // `evil.localhost.attacker.com` ends in `.com`, not `.localhost` — must be rejected.
        let response = test_router().handle(
            Request::new("GET", "/healthz")
                .with_header("Origin", "http://evil.localhost.attacker.com"),
        );

        assert_eq!(response.status, 403);
    }

    #[test]
    fn preview_serves_artifact_inside_cache_root() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-preview-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&cache_dir).unwrap();
        let artifact_path = cache_dir.join("preview.png");
        fs::write(&artifact_path, b"png-bytes").unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir.clone();
        config.paths.persistence_dir = state_dir;
        let app = Arc::new(App::new(config).unwrap());
        let router = Router::new(app);

        let response = router.handle(Request::new(
            "GET",
            format!(
                "{PREVIEW_PATH}?path={}",
                url::form_urlencoded::byte_serialize(artifact_path.to_string_lossy().as_bytes())
                    .collect::<String>()
            ),
        ));

        assert_eq!(response.status, 200);
        assert_eq!(response.body, b"png-bytes");
        assert_eq!(
            response.headers.get("content-type"),
            Some(&"image/png".to_string())
        );

        let _ = fs::remove_file(artifact_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn preview_sets_cache_control_and_etag_and_returns_304_on_match() {
        let temp_dir = std::env::temp_dir().join(format!(
            "xrayview-rs-preview-etag-{}",
            std::process::id()
        ));
        let cache_dir = temp_dir.join("cache");
        fs::create_dir_all(&cache_dir).unwrap();
        let artifact_path = cache_dir.join("preview.png");
        fs::write(&artifact_path, b"png-bytes").unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        let router = Router::new(Arc::new(App::new(config).unwrap()));
        let url = format!(
            "{PREVIEW_PATH}?path={}",
            url::form_urlencoded::byte_serialize(artifact_path.to_string_lossy().as_bytes())
                .collect::<String>()
        );

        let cold = router.handle(Request::new("GET", &url));
        assert_eq!(cold.status, 200);
        assert_eq!(
            cold.headers.get("cache-control"),
            Some(&"public, max-age=3600".to_string())
        );
        let etag = cold
            .headers
            .get("etag")
            .cloned()
            .expect("cold response missing ETag");

        let warm = router.handle(Request::new("GET", &url).with_header("If-None-Match", &etag));
        assert_eq!(warm.status, 304);
        assert!(warm.body.is_empty());
        assert_eq!(warm.headers.get("etag"), Some(&etag));

        let stale = router
            .handle(Request::new("GET", &url).with_header("If-None-Match", "\"stale-etag\""));
        assert_eq!(stale.status, 200);
        assert_eq!(stale.body, b"png-bytes");

        let _ = fs::remove_file(artifact_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn preview_rejects_path_outside_cache_root() {
        let temp_dir = std::env::temp_dir().join(format!(
            "xrayview-rs-preview-outside-{}",
            std::process::id()
        ));
        let cache_dir = temp_dir.join("cache");
        let outside_dir = temp_dir.join("outside");
        fs::create_dir_all(&cache_dir).unwrap();
        fs::create_dir_all(&outside_dir).unwrap();
        let artifact_path = outside_dir.join("preview.png");
        fs::write(&artifact_path, b"png-bytes").unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        let router = Router::new(Arc::new(App::new(config).unwrap()));

        let response = router.handle(Request::new(
            "GET",
            format!(
                "{PREVIEW_PATH}?path={}",
                url::form_urlencoded::byte_serialize(artifact_path.to_string_lossy().as_bytes())
                    .collect::<String>()
            ),
        ));

        assert_eq!(response.status, 403);

        let _ = fs::remove_file(artifact_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn structs_used_by_tests_stay_importable() {
        let _annotation = LineAnnotation {
            id: "line-1".to_string(),
            label: "Measurement 1".to_string(),
            source: AnnotationSource::Manual,
            start: AnnotationPoint { x: 0.0, y: 0.0 },
            end: AnnotationPoint { x: 1.0, y: 1.0 },
            editable: true,
            confidence: None,
            measurement: None,
        };
    }
}
