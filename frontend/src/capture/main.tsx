import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { Capture } from "@/capture/app";
import "@/index.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <Capture />
  </StrictMode>,
);
