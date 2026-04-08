import React from "react";
import ReactDOM from "react-dom/client";
import "../styles/tokens.css";
import "../styles/base.css";
import "../styles/utilities.css";
import "../styles/wailsPrototype.css";
import { WailsPrototypeApp } from "./App";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <WailsPrototypeApp />
  </React.StrictMode>,
);
