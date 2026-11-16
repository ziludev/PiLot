import { defineConfig } from "mygo-cli";

// Pitta's window is drawn by mygo/ui from Go; there is no web frontend to
// build, so no devUrl, buildCommand or frontendDist.
export default defineConfig({
  // The React client's identifier is kept so macOS treats this as the same app.
  name: "Pitta",
  identifier: "com.example.agent",
  version: "0.1.0",
  out: "build",
});
