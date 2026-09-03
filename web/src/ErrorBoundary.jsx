import React from "react";

// ErrorBoundary catches render errors in the page content so a single broken
// page shows a recoverable message instead of blanking the entire app. The
// content is keyed by view in App, so navigating to another page remounts this
// and clears the error automatically.
export default class ErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error("UI error:", error, info);
  }

  render() {
    if (this.state.error) {
      return (
        <div className="err-boundary">
          <h2>Something went wrong on this page.</h2>
          <p className="subtle">{String(this.state.error.message || this.state.error)}</p>
          <div className="row" style={{ gap: 10, marginTop: 14 }}>
            <button className="btn-sm btn-primary" onClick={() => this.setState({ error: null })}>Retry</button>
            <button className="btn-sm" onClick={() => window.location.reload()}>Reload app</button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
