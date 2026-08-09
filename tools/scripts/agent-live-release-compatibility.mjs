#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

const generatedProtocolURL = new URL(
  "../../packages/agent/daemon/liveprotocol/protocol_compatibility.gen.json",
  import.meta.url
);
const releaseBootstrapURL = new URL(
  "../release/agent-live-protocol-bootstrap.json",
  import.meta.url
);
const releaseBootstrapTags = {
  desktopRc: ["v0.2.21-rc.0"],
  mobile: ["tutti-mobile-v0.1.8"]
};

export async function loadGeneratedAgentLiveProtocol() {
  return validateAgentLiveProtocol(
    JSON.parse(await readFile(generatedProtocolURL, "utf8")),
    "generated Agent live protocol"
  );
}

export function validateAgentLiveProtocol(value, label) {
  if (!isRecord(value)) {
    throw new Error(`${label} must be an object`);
  }
  const currentRevision = requireRevision(
    value.currentRevision,
    `${label}.currentRevision`
  );
  if (!Array.isArray(value.acceptedRevisions)) {
    throw new Error(`${label}.acceptedRevisions must be an array`);
  }
  const acceptedRevisions = value.acceptedRevisions.map((revision, index) =>
    requireRevision(revision, `${label}.acceptedRevisions[${index}]`)
  );
  if (new Set(acceptedRevisions).size !== acceptedRevisions.length) {
    throw new Error(`${label}.acceptedRevisions must be unique`);
  }
  if (!acceptedRevisions.includes(currentRevision)) {
    throw new Error(`${label}.acceptedRevisions must include currentRevision`);
  }
  return { currentRevision, acceptedRevisions };
}

export function releaseAgentLiveProtocol(document, label) {
  if (!isRecord(document)) {
    throw new Error(`${label} release pointer must be an object`);
  }
  return validateAgentLiveProtocol(
    document.agentLiveProtocol,
    `${label} release pointer agentLiveProtocol`
  );
}

export async function releaseAgentLiveProtocolWithBootstrap(
  document,
  label,
  releaseKind
) {
  validateReleasePointerIdentity(document, label, releaseKind);
  if (isRecord(document?.agentLiveProtocol)) {
    return releaseAgentLiveProtocol(document, label);
  }
  const bootstrap = validateReleaseBootstrap(
    JSON.parse(await readFile(releaseBootstrapURL, "utf8"))
  );
  const tag = String(document?.tag ?? "").trim();
  const protocol = bootstrap.releases[releaseKind][tag];
  if (!protocol) {
    throw new Error(
      `${label} release pointer ${tag || "<missing tag>"} has no agentLiveProtocol metadata or exact bootstrap entry`
    );
  }
  return validateAgentLiveProtocol(
    protocol,
    `${label} release pointer bootstrap ${tag}`
  );
}

export function validateReleasePointerIdentity(document, label, releaseKind) {
  if (!isRecord(document)) {
    throw new Error(`${label} release pointer must be an object`);
  }
  switch (releaseKind) {
    case "desktopRc": {
      if (document.schemaVersion !== "tutti.desktop.release.latest.v1") {
        throw new Error(`${label} release pointer schemaVersion is invalid`);
      }
      const version = requireExactString(
        document.version,
        `${label} release pointer version`
      );
      if (!/^\d+\.\d+\.\d+-rc\.\d+$/.test(version)) {
        throw new Error(
          `${label} release pointer version must be an RC semver`
        );
      }
      if (
        document.tag !== `v${version}` ||
        document.channel !== "rc" ||
        document.prerelease !== true
      ) {
        throw new Error(`${label} release pointer must identify Desktop RC`);
      }
      return;
    }
    case "mobile": {
      if (document.schemaVersion !== "tutti.android.mobile.latest.v1") {
        throw new Error(`${label} release pointer schemaVersion is invalid`);
      }
      const versionName = requireExactString(
        document.versionName,
        `${label} release pointer versionName`
      );
      if (!/^\d+\.\d+\.\d+$/.test(versionName)) {
        throw new Error(
          `${label} release pointer versionName must be a stable semver`
        );
      }
      if (
        document.tag !== `tutti-mobile-v${versionName}` ||
        document.packageName !== "sh.tutti.mobile" ||
        !Number.isSafeInteger(document.versionCode) ||
        document.versionCode <= 0
      ) {
        throw new Error(`${label} release pointer must identify Mobile`);
      }
      return;
    }
    default:
      throw new Error(`Unknown Agent live release kind: ${releaseKind}`);
  }
}

export function validateReleaseBootstrap(bootstrap) {
  if (
    bootstrap?.schemaVersion !== "tutti.agent-live.release-bootstrap.v1" ||
    !isRecord(bootstrap.releases) ||
    !sameStringArray(
      Object.keys(bootstrap.releases).sort(),
      Object.keys(releaseBootstrapTags).sort()
    )
  ) {
    throw new Error("Agent live release bootstrap contract is invalid");
  }
  for (const [releaseKind, expectedTags] of Object.entries(
    releaseBootstrapTags
  )) {
    const releases = bootstrap.releases[releaseKind];
    if (
      !isRecord(releases) ||
      !sameStringArray(Object.keys(releases).sort(), [...expectedTags].sort())
    ) {
      throw new Error(
        `Agent live release bootstrap ${releaseKind} entries are invalid`
      );
    }
    for (const tag of expectedTags) {
      validateAgentLiveProtocol(
        releases[tag],
        `Agent live release bootstrap ${releaseKind}.${tag}`
      );
    }
  }
  return bootstrap;
}

export function assertAgentLiveProtocolExactMatch({ actual, expected }) {
  if (
    actual.currentRevision === expected.currentRevision &&
    sameStringArray(actual.acceptedRevisions, expected.acceptedRevisions)
  ) {
    return;
  }
  throw new Error(
    "Generated Mobile Agent live protocol does not exactly match the published Android pointer: " +
      `generated current=${actual.currentRevision} accepted=${actual.acceptedRevisions.join(",")}; ` +
      `published current=${expected.currentRevision} accepted=${expected.acceptedRevisions.join(",")}`
  );
}

export function canReachAgentLiveProtocol({ desktop, mobile }) {
  return (
    desktop.acceptedRevisions.includes(mobile.currentRevision) ||
    mobile.acceptedRevisions.includes(desktop.currentRevision)
  );
}

export function assertAgentLiveReleaseCompatibility({ desktop, mobile }) {
  if (canReachAgentLiveProtocol({ desktop, mobile })) return;
  throw new Error(
    "Mobile and Desktop RC Agent live protocols are unreachable: " +
      `mobile current=${mobile.currentRevision} accepted=${mobile.acceptedRevisions.join(",")}; ` +
      `desktop current=${desktop.currentRevision} accepted=${desktop.acceptedRevisions.join(",")}`
  );
}

function requireRevision(value, label) {
  const revision = String(value ?? "").trim();
  if (!/^sha256:[0-9a-f]{16}$/.test(revision)) {
    throw new Error(`${label} must be a truncated SHA-256 protocol revision`);
  }
  return revision;
}

function requireExactString(value, label) {
  if (typeof value !== "string" || !value || value.trim() !== value) {
    throw new Error(`${label} must be a non-empty exact string`);
  }
  return value;
}

function sameStringArray(left, right) {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  );
}

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function parseCompatibilityArguments(argv) {
  const result = {};
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--mobile-generated") {
      result.mobileGenerated = true;
      continue;
    }
    if (
      argument !== "--desktop" &&
      argument !== "--mobile" &&
      argument !== "--released-mobile"
    ) {
      throw new Error(`Unexpected argument: ${argument}`);
    }
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) {
      throw new Error(`Missing value for ${argument}`);
    }
    result[
      argument === "--released-mobile" ? "releasedMobile" : argument.slice(2)
    ] = value;
    index += 1;
  }
  if (result.mobile && result.mobileGenerated) {
    throw new Error("--mobile and --mobile-generated are mutually exclusive");
  }
  if (result.releasedMobile && !result.mobileGenerated) {
    throw new Error("--released-mobile requires --mobile-generated");
  }
  return result;
}

async function main() {
  const args = parseCompatibilityArguments(process.argv.slice(2));
  if (!args.desktop) {
    throw new Error("--desktop is required");
  }
  if (!args.mobile && !args.mobileGenerated) {
    throw new Error("--mobile or --mobile-generated is required");
  }
  const desktopDocument = JSON.parse(
    await readFile(path.resolve(args.desktop), "utf8")
  );
  const desktop = await releaseAgentLiveProtocolWithBootstrap(
    desktopDocument,
    "Desktop RC",
    "desktopRc"
  );
  const mobile = args.mobileGenerated
    ? await loadGeneratedAgentLiveProtocol()
    : await releaseAgentLiveProtocolWithBootstrap(
        JSON.parse(await readFile(path.resolve(args.mobile), "utf8")),
        "Mobile",
        "mobile"
      );
  if (args.releasedMobile) {
    const releasedMobile = await releaseAgentLiveProtocolWithBootstrap(
      JSON.parse(await readFile(path.resolve(args.releasedMobile), "utf8")),
      "Published Android",
      "mobile"
    );
    assertAgentLiveProtocolExactMatch({
      actual: mobile,
      expected: releasedMobile
    });
  }
  assertAgentLiveReleaseCompatibility({ desktop, mobile });
  console.log(
    `Agent live release pair is reachable: mobile=${mobile.currentRevision} desktop=${desktop.currentRevision}`
  );
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  void main().catch((error) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  });
}
