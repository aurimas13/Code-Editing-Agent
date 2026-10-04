// Incremental parser for server-sent events read from a fetch() body.
// EventSource cannot send a POST or an Authorization header, so the
// playground reads the stream itself.

export type SSEMessage = { event: string; data: string };

export class SSEParser {
  private buffer = "";

  /** Feeds a chunk of text and returns the complete messages it finished. */
  feed(chunk: string): SSEMessage[] {
    this.buffer += chunk.replace(/\r\n/g, "\n");
    const out: SSEMessage[] = [];
    for (;;) {
      const boundary = this.buffer.indexOf("\n\n");
      if (boundary < 0) break;
      const block = this.buffer.slice(0, boundary);
      this.buffer = this.buffer.slice(boundary + 2);

      let event = "message";
      const data: string[] = [];
      for (const line of block.split("\n")) {
        if (line === "" || line.startsWith(":")) continue; // comment / keep-alive
        const colon = line.indexOf(":");
        const field = colon < 0 ? line : line.slice(0, colon);
        const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
        if (field === "event") event = value;
        else if (field === "data") data.push(value);
      }
      if (data.length > 0) out.push({ event, data: data.join("\n") });
    }
    return out;
  }
}
