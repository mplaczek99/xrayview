import htmx from "htmx.org";
import { mountHtmxApp } from "./app/htmxApp";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/utilities.css";

htmx.config.allowEval = false;

const root = document.getElementById("app");
if (!root) {
  throw new Error("missing #app mount element");
}

mountHtmxApp(root, htmx);
