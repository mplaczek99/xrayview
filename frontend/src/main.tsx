import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./app/App";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/utilities.css";

interface RootErrorBoundaryState {
  error: Error | null;
}

class RootErrorBoundary extends React.Component<
  React.PropsWithChildren,
  RootErrorBoundaryState
> {
  state: RootErrorBoundaryState = {
    error: null,
  };

  static getDerivedStateFromError(error: Error): RootErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.error("xrayview frontend render error", error);
  }

  render() {
    if (this.state.error) {
      return (
        <div className="viewer-stage">
          <div className="viewer-placeholder">
            <div className="viewer-placeholder__title">Frontend Error</div>
            <p className="viewer-placeholder__copy">
              {this.state.error.message}
            </p>
            <button
              className="button button--primary"
              type="button"
              onClick={() => window.location.reload()}
            >
              Reload
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}

// StrictMode helps catch React-side effect mistakes before the same code runs
// inside the packaged Wails shell.
ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <RootErrorBoundary>
      <App />
    </RootErrorBoundary>
  </React.StrictMode>,
);
