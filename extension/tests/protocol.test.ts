import { describe, expect, it } from "vitest";
import corpus from "../../contracts/cases.json";
import { decodeJSON, decodeMessage, decodeRequest, MAX_MESSAGE_BYTES, ProtocolDecodeError, type ValidationContext } from "../src/protocol";

// Removing strict fields, contextual membership or progress constraints must
// turn the corresponding hand-authored rejection fixture into an acceptance.
describe("shared protocol v1 corpus", () => {
  for (const fixture of corpus.cases) {
    it(fixture.name, () => {
      const context = "useContext" in fixture && fixture.useContext ? corpus.context as ValidationContext : undefined;
      const decode = () => fixture.direction === "request" ? decodeRequest(fixture.value, context) : decodeMessage(fixture.value);
      if (fixture.valid) expect(decode()).toEqual(fixture.value);
      else {
        try { decode(); expect.fail("Invalid fixture was accepted"); }
        catch (error) {
          expect(error).toBeInstanceOf(ProtocolDecodeError);
          expect((error as ProtocolDecodeError).code).toBe("errorCode" in fixture ? fixture.errorCode : undefined);
        }
      }
    });
  }
});

describe("frame and contextual boundaries", () => {
  it("RejectOversizedFrame counts UTF-8 bytes rather than characters", () => {
    const json = JSON.stringify({ protocolVersion: 1, requestId: "req-a", operation: "handshake", payload: { extensionVersion: "é".repeat(MAX_MESSAGE_BYTES / 2) } });
    expect(() => decodeJSON(json, "request")).toThrowError(expect.objectContaining({ code: "MESSAGE_TOO_LARGE" }));
  });

  it("accepts a frame exactly at the byte limit before strict field decoding", () => {
    const json = JSON.stringify(corpus.cases[0].value);
    const atLimit = json + " ".repeat(MAX_MESSAGE_BYTES - new TextEncoder().encode(json).byteLength);
    expect(decodeJSON(atLimit, "request")).toEqual(corpus.cases[0].value);
    expect(() => decodeJSON(atLimit + " ", "request")).toThrowError(expect.objectContaining({ code: "MESSAGE_TOO_LARGE" }));
  });

  it("rejects malformed JSON and trailing JSON objects", () => {
    expect(() => decodeJSON("{", "request")).toThrowError(expect.objectContaining({ code: "INVALID_MESSAGE" }));
    expect(() => decodeJSON("{}{}", "request")).toThrowError(expect.objectContaining({ code: "INVALID_MESSAGE" }));
  });

  it("rejects expired selection instead of substituting a format", () => {
    const fixture = corpus.cases.find(value => value.name === "DecodeValidStartVideo")!;
    expect(() => decodeRequest(fixture.value, { ...corpus.context as ValidationContext, now: "2026-10-13T12:05:00Z" })).toThrowError(expect.objectContaining({ code: "ANALYSIS_EXPIRED" }));
  });

  it("rejects MP3 when the registered media has no audio", () => {
    const fixture = corpus.cases.find(value => value.name === "DecodeValidMp3192")!;
    const context = structuredClone(corpus.context) as ValidationContext;
    context.analysis!.items[0].hasAudio = false;
    expect(() => decodeRequest(fixture.value, context)).toThrowError(expect.objectContaining({ code: "NO_AUDIO" }));
  });

  it("rejects non-finite progress supplied as a JavaScript object", () => {
    const fixture = structuredClone(corpus.cases.find(value => value.name === "DecodeKnownDownloadProgress")!.value) as Record<string, unknown>;
    fixture.sequence = Infinity;
    expect(() => decodeMessage(fixture)).toThrowError(expect.objectContaining({ code: "INVALID_MESSAGE" }));
  });
});
