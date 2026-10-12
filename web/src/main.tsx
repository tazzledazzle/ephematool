import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { setPageLoadContext } from "@/api/client";
import { initTelemetry } from "@/telemetry";
import { startPageLoad } from "@/telemetry/pageLoad";
import App from "./App";
import "./index.css";

// Telemetry must be live before React renders so the page load is traced.
initTelemetry();

// Requests made while the first screen renders are children of this span. The
// screen that finishes loading calls pageLoadDone(); the 10s auto-end is the backstop.
const pageLoad = startPageLoad();
setPageLoadContext(pageLoad.context, pageLoad.end);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
