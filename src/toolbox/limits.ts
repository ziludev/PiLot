// The retained-output limits of pi-durable's coding tools. They are copied rather
// than imported because the package exposes the module only internally; both paths
// must keep the same window, and the eval fails if they drift.
export const DEFAULT_MAX_LINES = 2000;
export const DEFAULT_MAX_BYTES = 50 * 1024;
