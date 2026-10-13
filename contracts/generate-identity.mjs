// One-time prototype identity generation. Only the public SPKI key is retained.
// Do not regenerate an existing release identity: it changes the Chrome ID.
import { generateKeyPairSync, createHash } from "node:crypto";
import { writeFileSync, existsSync } from "node:fs";
const target = new URL("extension-identity.json", import.meta.url);
if (existsSync(target)) throw new Error("An extension identity already exists");
const { publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const der = publicKey.export({ type: "spki", format: "der" });
const digest = createHash("sha256").update(der).digest().subarray(0, 16);
const extensionId = [...digest].flatMap(byte => [byte >> 4, byte & 15]).map(nibble => String.fromCharCode(97 + nibble)).join("");
writeFileSync(target, JSON.stringify({ hostName: "com.dwnl.host", protocolVersion: 1, extensionId, key: der.toString("base64") }, null, 2) + "\n", { flag: "wx" });
console.log(extensionId);
