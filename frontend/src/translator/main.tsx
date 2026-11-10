import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { Translator } from "@/translator/app";
import "@/index.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <Translator />
  </StrictMode>,
);
