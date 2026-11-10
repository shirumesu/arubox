import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { Screenshot } from "@/screenshot/app";
import "@/index.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <Screenshot />
  </StrictMode>,
);
