import { defineConfig } from "mygo-cli";

// PiLot's window is drawn by mygo/ui from Go; there is no web frontend to
// build, so no devUrl, buildCommand or frontendDist.
export default defineConfig({
  // The webview client's identifier is kept so macOS treats this as the same app.
  name: "PiLot",
  identifier: "com.example.agent",
  version: "0.1.0",
  out: "build",
  // The app icon scripts/render-icon.sh renders from resources/pilot-app-icon.svg.
  icon: "resources/icon.png",
  // pilot://chat/123 opens that chat; ?window=new gives it its own window.
  urlSchemes: ["pilot"],
});
