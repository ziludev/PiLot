import { defineConfig } from "mygo-cli";

// Pitta's window is drawn by mygo/ui from Go; there is no web frontend to
// build, so no devUrl, buildCommand or frontendDist.
export default defineConfig({
  // The webview client's identifier is kept so macOS treats this as the same app.
  name: "Pitta",
  identifier: "com.example.agent",
  version: "0.1.0",
  out: "build",
  // The mark scripts/make-icon.py draws (the same amber face the rail shows).
  icon: "resources/icon.png",
  // pitta://chat/123 opens that chat; ?window=new gives it its own window.
  urlSchemes: ["pitta"],
});
