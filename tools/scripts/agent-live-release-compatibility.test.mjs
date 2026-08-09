import assert from "node:assert/strict";
import test from "node:test";

import {
  assertAgentLiveProtocolExactMatch,
  assertAgentLiveReleaseCompatibility,
  canReachAgentLiveProtocol,
  loadGeneratedAgentLiveProtocol,
  parseCompatibilityArguments,
  releaseAgentLiveProtocol,
  releaseAgentLiveProtocolWithBootstrap,
  validateReleaseBootstrap,
  validateReleasePointerIdentity
} from "./agent-live-release-compatibility.mjs";

const current = "sha256:1111111111111111";
const previous = "sha256:2222222222222222";
const sharedThird = "sha256:3333333333333333";

function desktopRcPointer({ agentLiveProtocol, tag = "v1.2.3-rc.4" } = {}) {
  const version = tag.slice(1);
  return {
    ...(agentLiveProtocol ? { agentLiveProtocol } : {}),
    channel: "rc",
    prerelease: true,
    schemaVersion: "tutti.desktop.release.latest.v1",
    tag,
    version
  };
}

function mobilePointer({
  agentLiveProtocol,
  tag = "tutti-mobile-v1.2.3",
  versionCode = 42
} = {}) {
  const versionName = tag.replace("tutti-mobile-v", "");
  return {
    ...(agentLiveProtocol ? { agentLiveProtocol } : {}),
    packageName: "sh.tutti.mobile",
    schemaVersion: "tutti.android.mobile.latest.v1",
    tag,
    versionCode,
    versionName
  };
}

test("loads the generated Agent live release protocol", async () => {
  const protocol = await loadGeneratedAgentLiveProtocol();
  assert.equal(protocol.currentRevision, "sha256:b29022aba44d33cb");
  assert.deepEqual(protocol.acceptedRevisions, [
    "sha256:b29022aba44d33cb",
    "sha256:7101e69f2559036c"
  ]);
});

test("selects the generated Mobile protocol for an iOS release gate", () => {
  assert.deepEqual(
    parseCompatibilityArguments([
      "--desktop",
      "desktop.json",
      "--mobile-generated",
      "--released-mobile",
      "mobile.json"
    ]),
    {
      desktop: "desktop.json",
      mobileGenerated: true,
      releasedMobile: "mobile.json"
    }
  );
  assert.throws(
    () =>
      parseCompatibilityArguments([
        "--desktop",
        "desktop.json",
        "--mobile",
        "mobile.json",
        "--mobile-generated"
      ]),
    /mutually exclusive/
  );
  assert.throws(
    () =>
      parseCompatibilityArguments([
        "--desktop",
        "desktop.json",
        "--mobile",
        "mobile.json",
        "--released-mobile",
        "published.json"
      ]),
    /requires --mobile-generated/
  );
});

test("accepts both reachable handshake directions", () => {
  assert.equal(
    canReachAgentLiveProtocol({
      desktop: { currentRevision: previous, acceptedRevisions: [previous] },
      mobile: {
        currentRevision: current,
        acceptedRevisions: [current, previous]
      }
    }),
    true
  );
  assert.equal(
    canReachAgentLiveProtocol({
      desktop: {
        currentRevision: current,
        acceptedRevisions: [current, previous]
      },
      mobile: { currentRevision: previous, acceptedRevisions: [previous] }
    }),
    true
  );
});

test("rejects a third shared revision that the handshake cannot select", () => {
  const desktop = {
    currentRevision: current,
    acceptedRevisions: [current, sharedThird]
  };
  const mobile = {
    currentRevision: previous,
    acceptedRevisions: [previous, sharedThird]
  };
  assert.equal(canReachAgentLiveProtocol({ desktop, mobile }), false);
  assert.throws(
    () => assertAgentLiveReleaseCompatibility({ desktop, mobile }),
    /protocols are unreachable/
  );
});

test("requires release metadata to include its current revision", () => {
  assert.throws(
    () =>
      releaseAgentLiveProtocol(
        {
          agentLiveProtocol: {
            acceptedRevisions: [previous],
            currentRevision: current
          }
        },
        "Mobile"
      ),
    /must include currentRevision/
  );
});

test("release pointer identity rejects non-RC Desktop and malformed Mobile documents", () => {
  assert.doesNotThrow(() =>
    validateReleasePointerIdentity(
      desktopRcPointer(),
      "Desktop RC",
      "desktopRc"
    )
  );
  assert.throws(
    () =>
      validateReleasePointerIdentity(
        {
          ...desktopRcPointer(),
          channel: "stable",
          prerelease: false,
          tag: "v1.2.3",
          version: "1.2.3"
        },
        "Desktop RC",
        "desktopRc"
      ),
    /must be an RC semver|must identify Desktop RC/
  );
  assert.throws(
    () =>
      validateReleasePointerIdentity(
        { ...mobilePointer(), packageName: "wrong.package" },
        "Mobile",
        "mobile"
      ),
    /must identify Mobile/
  );
});

test("requires the generated iOS protocol to exactly match the Android pointer", () => {
  const generated = {
    acceptedRevisions: [current, previous],
    currentRevision: current
  };
  assert.doesNotThrow(() =>
    assertAgentLiveProtocolExactMatch({
      actual: generated,
      expected: generated
    })
  );
  assert.throws(
    () =>
      assertAgentLiveProtocolExactMatch({
        actual: generated,
        expected: {
          acceptedRevisions: [previous],
          currentRevision: previous
        }
      }),
    /does not exactly match/
  );
});

test("release bootstrap allowlist is exact and bounded", () => {
  const protocol = {
    acceptedRevisions: [current],
    currentRevision: current
  };
  const valid = {
    releases: {
      desktopRc: { "v0.2.21-rc.0": protocol },
      mobile: { "tutti-mobile-v0.1.8": protocol }
    },
    schemaVersion: "tutti.agent-live.release-bootstrap.v1"
  };
  assert.equal(validateReleaseBootstrap(valid), valid);
  assert.throws(
    () =>
      validateReleaseBootstrap({
        ...valid,
        releases: {
          ...valid.releases,
          mobile: {
            ...valid.releases.mobile,
            "tutti-mobile-v0.1.9": protocol
          }
        }
      }),
    /entries are invalid/
  );
});

test("bootstraps only the exact existing Mobile and Desktop RC pointers", async () => {
  assert.deepEqual(
    await releaseAgentLiveProtocolWithBootstrap(
      mobilePointer({ tag: "tutti-mobile-v0.1.8", versionCode: 8 }),
      "Mobile",
      "mobile"
    ),
    {
      acceptedRevisions: ["sha256:b29022aba44d33cb"],
      currentRevision: "sha256:b29022aba44d33cb"
    }
  );
  assert.deepEqual(
    await releaseAgentLiveProtocolWithBootstrap(
      desktopRcPointer({ tag: "v0.2.21-rc.0" }),
      "Desktop RC",
      "desktopRc"
    ),
    {
      acceptedRevisions: ["sha256:7101e69f2559036c"],
      currentRevision: "sha256:7101e69f2559036c"
    }
  );
  await assert.rejects(
    () =>
      releaseAgentLiveProtocolWithBootstrap(
        mobilePointer({ tag: "tutti-mobile-v0.1.9", versionCode: 9 }),
        "Mobile",
        "mobile"
      ),
    /no agentLiveProtocol metadata or exact bootstrap entry/
  );
});
