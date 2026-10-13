import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { mkdir, mkdtemp, readFile, readdir } from "node:fs/promises";
import { createHash } from "node:crypto";
import path from "node:path";
import { afterEach, beforeAll, describe, expect, test } from "vitest";
import { decodeMessage, MAX_MESSAGE_BYTES, type JobEvent, type Response } from "../../extension/src/protocol.js";
import { ChromeNativeClient } from "../../extension/src/controller.js";
import type { BrowserPort } from "../../extension/src/browser.js";

const project = process.cwd();
const binary = path.join(project, ".tools/integration/dwnl-test-harness.exe");
const bins = path.join(project, ".tools/media/bin");
const origin = "chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/";
const fixtureURL = "https://youtu.be/dwnl-local-fixture";
const children = new Set<NativeProcess>();

// Test-only stdio transport. The production package never imports this module.
class NativeProcess {
  readonly child: ChildProcessWithoutNullStreams;
  readonly messages: (Response | JobEvent)[] = [];
  readonly listeners = new Set<(message: unknown) => void>();
  failure?: Error;
  stderr = "";
  private bytes = Buffer.alloc(0);
  private subscribers = new Set<() => void>();
  constructor(root: string, requestedOrigin = origin) {
    this.child = spawn(binary, [root, bins, requestedOrigin], { cwd: project, windowsHide: true });
    children.add(this);
    this.child.stdout.on("data", chunk => {
      try {
        this.bytes = Buffer.concat([this.bytes, chunk]);
        while (this.bytes.length >= 4) {
          const size = this.bytes.readUInt32LE(0);
          if (!size || size > MAX_MESSAGE_BYTES) throw new Error("Invalid native output frame");
          if (this.bytes.length < size + 4) break;
          const body = this.bytes.subarray(4, size + 4);
          this.bytes = this.bytes.subarray(size + 4);
          const decoded = decodeMessage(JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(body)));
          this.messages.push(decoded);
          for (const listener of this.listeners) listener(decoded);
        }
      } catch (error) { this.failure = error as Error; }
      for (const notify of this.subscribers) notify();
    });
    this.child.on("error", error => { this.failure = error; for (const notify of this.subscribers) notify(); });
    this.child.stderr.on("data", chunk => { this.stderr = (this.stderr + chunk.toString()).slice(-2048); });
    this.child.once("exit", code => {
      if (code !== 0) { this.failure = new Error(`Isolated host exited (${code}): ${this.stderr}`); for (const notify of this.subscribers) notify(); }
    });
  }
  send(value: unknown, split = false) {
    const body = Buffer.from(JSON.stringify(value));
    const header = Buffer.alloc(4); header.writeUInt32LE(body.length);
    const frame = Buffer.concat([header, body]);
    if (split) { this.child.stdin.write(frame.subarray(0, 2)); this.child.stdin.write(frame.subarray(2, 9)); this.child.stdin.write(frame.subarray(9)); }
    else this.child.stdin.write(frame);
  }
  wait<T extends Response | JobEvent>(predicate: (message: Response | JobEvent) => message is T): Promise<T>;
  wait(predicate: (message: Response | JobEvent) => boolean): Promise<Response | JobEvent>;
  wait(predicate: (message: Response | JobEvent) => boolean): Promise<Response | JobEvent> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.subscribers.delete(check); reject(new Error("Native response timeout")); }, 20000);
      const check = () => {
        if (this.failure) { clearTimeout(timer); this.subscribers.delete(check); reject(this.failure); return; }
        const found = this.messages.find(predicate);
        if (found) { clearTimeout(timer); this.subscribers.delete(check); resolve(found); }
      };
      this.subscribers.add(check); check();
    });
  }
  response(requestId: string) { return this.wait((m): m is Response => m.kind === "response" && m.requestId === requestId); }
  async close() {
    if (this.child.exitCode !== null) { children.delete(this); return; }
    const exited = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => { this.child.kill(); reject(new Error("Host did not exit on EOF")); }, 5000);
      this.child.once("exit", () => { clearTimeout(timer); resolve(); });
    });
    this.child.stdin.end(); await exited; children.delete(this);
  }
}

async function isolatedRoot() {
  await mkdir(path.join(project, ".tools/tmp"), { recursive: true });
  return mkdtemp(path.join(project, ".tools/tmp/dwnl-native-integration-"));
}
async function connect(root: string) {
  const client = new NativeProcess(root);
  client.send({ protocolVersion: 1, requestId: "handshake-1", operation: "handshake", payload: { extensionVersion: "0.1.0" } }, true);
  const response = await client.response("handshake-1");
  expect(response.error).toBeUndefined();
  expect(response.operation).toBe("handshake");
  if (response.operation !== "handshake" || !response.result) throw new Error("Handshake failed");
  return { client, session: response.hostSessionId, handshake: response.result };
}
function request(client: NativeProcess, session: string, requestId: string, operation: string, payload: unknown) {
  client.send({ protocolVersion: 1, requestId, hostSessionId: session, operation, payload });
}

beforeAll(async () => { expect((await readFile(binary)).subarray(0, 2).toString()).toBe("MZ"); });
afterEach(async () => { await Promise.all([...children].map(child => child.close())); });

describe("actual Go stdio core with isolated local media", () => {
  test("production TypeScript native client exchanges real Go control frames", async () => {
    const process = new NativeProcess(await isolatedRoot());
    const disconnected = new Set<() => void>();
    process.child.once("exit", () => { for (const listener of disconnected) listener(); });
    const port: BrowserPort = {
      postMessage: value => process.send(value, true),
      disconnect: () => process.child.stdin.end(),
      onMessage: { addListener: listener => { process.listeners.add(listener); }, removeListener: listener => { process.listeners.delete(listener); } },
      onDisconnect: { addListener: listener => { disconnected.add(listener); }, removeListener: listener => { disconnected.delete(listener); } },
    };
    const client = new ChromeNativeClient(name => { expect(name).toBe("com.dwnl.host"); return port; }, () => undefined, 20000);
    const handshake = await client.request("handshake", { extensionVersion: "0.1.0" }, "client-handshake");
    expect(handshake.operation === "handshake" && handshake.result?.capabilities.networkAccess).toBe(false);
    const status = await client.request("status", {}, "client-status");
    expect(status.operation === "status" && status.result?.snapshot).toBeNull();
    const blocked = await client.request("analyze", { url: "https://youtu.be/public-case-not-authorized" }, "client-analyze");
    expect(blocked.error?.code).toBe("NETWORK_POLICY_BLOCKED");
  }, 30000);

  test("split handshake, honest network gate and an empty status", async () => {
    const { client, session, handshake } = await connect(await isolatedRoot());
    expect(handshake.capabilities.networkAccess).toBe(false);
    request(client, session, "status-1", "status", {});
    const status = await client.response("status-1");
    expect(status.operation === "status" && status.result?.snapshot).toBeNull();
    request(client, session, "online-1", "analyze", { url: "https://youtu.be/public-case-not-authorized" });
    expect((await client.response("online-1")).error?.code).toBe("NETWORK_POLICY_BLOCKED");
  }, 30000);

  test("lost start acknowledgement reconciles by request ID without a duplicate output", async () => {
    const root = await isolatedRoot();
    const { client, session, handshake } = await connect(root);
    request(client, session, "analyze-local", "analyze", { url: fixtureURL });
    const response = await client.response("analyze-local");
    if (response.operation !== "analyze" || !response.result) throw new Error("Local fixture analysis failed");
    const analysis = response.result, item = analysis.items[0];
    const payload = { analysisId: analysis.analysisId, itemId: item.itemId, mode: "mp3", bitrateKbps: 192, destinationId: handshake.defaultDestination.destinationId };
    request(client, session, "start-lost", "start", payload);
    // Deliberately ignore the acknowledgement; the next request resolves its identity.
    request(client, session, "status-lost", "status", { startRequestId: "start-lost" });
    const status = await client.response("status-lost");
    if (status.operation !== "status" || !status.result?.snapshot) throw new Error("Lost start not found");
    const jobId = status.result.snapshot.jobId;
    request(client, session, "start-lost", "start", payload);
    const completion = await client.wait((m): m is JobEvent => m.kind === "event" && m.jobId === jobId && m.state === "completed");
    expect(completion.completedFile?.mp3BitrateKbps).toBe(192);
    const output = completion.completedFile!;
    const actual = await readFile(output.path);
    expect(actual.length).toBe(output.sizeBytes);
    expect(createHash("sha256").update(actual).digest("hex")).toBe(output.sha256);
    expect((await readdir(path.join(root, "downloads"))).filter(name => name.endsWith(".mp3"))).toHaveLength(1);
    await client.close();
    const reopened = await connect(root);
    request(reopened.client, reopened.session, "reconcile-old", "reconcile", { previousHostSessionId: session, jobId });
    const reconciliation = await reopened.client.response("reconcile-old");
    expect(reconciliation.operation === "reconcile" && reconciliation.result?.outcome).toBe("confirmed");
    expect((await readdir(path.join(root, "downloads"))).filter(name => name.endsWith(".mp3"))).toHaveLength(1);
  }, 60000);
});
