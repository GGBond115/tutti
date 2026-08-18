import assert from "node:assert/strict";
import test from "node:test";

import { projectAuthorizationQrCodeDataUrl } from "./connectorAuthorizationQrCode.ts";

test("projects an opaque QR payload into a closed image field", () => {
  const payload = "https://example.com/authorize?opaque=secret-value";
  const dataUrl = projectAuthorizationQrCodeDataUrl({
    protocol: "tutti.connector.authorization.view.v1",
    viewId: "qr-1",
    view: {
      type: "qr_code",
      source: { type: "payload", value: payload },
      refreshable: true
    }
  });

  assert.match(dataUrl ?? "", /^data:image\/gif;base64,/);
  assert.doesNotMatch(dataUrl ?? "", /secret-value/);
});

test("passes through a daemon-projected PNG without exposing QR dependencies", () => {
  assert.equal(
    projectAuthorizationQrCodeDataUrl({
      protocol: "tutti.connector.authorization.view.v1",
      viewId: "qr-2",
      view: {
        type: "qr_code",
        source: { type: "png_base64", value: "opaque-png" }
      }
    }),
    "data:image/png;base64,opaque-png"
  );
});
