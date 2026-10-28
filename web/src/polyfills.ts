import { Buffer } from "./nodeshim";

// The renderer references Node's global Buffer (Buffer.byteLength) directly.
// Augment, don't replace, whatever Buffer already exists.
const g = globalThis as any;
if (g.Buffer === undefined) {
  g.Buffer = Buffer;
} else if (typeof g.Buffer.byteLength !== "function") {
  g.Buffer.byteLength = Buffer.byteLength;
}
g.process ??= { platform: "browser", env: {} };
