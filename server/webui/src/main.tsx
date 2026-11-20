import React from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { AuthProvider } from "./auth";
import App from "./App";
import "./styles.css";
import { setHevcCapable } from "./lib/api";
import { detectHevc } from "./lib/whep";

// Birinchi ulanishda brauzer kodek imkoniyatini aniqlaymiz (§16.4).
void detectHevc().then(setHevcCapable);

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <App />
      </AuthProvider>
    </BrowserRouter>
  </React.StrictMode>,
);
