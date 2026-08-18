import type { AuthorizationViewEnvelopeV1 } from "@tutti-os/connector-authorization-protocol/v1";
import qrcode from "qrcode-generator";

/** Projects opaque QR authorization payloads into an image outside React. */
export function projectAuthorizationQrCodeDataUrl(
  envelope: AuthorizationViewEnvelopeV1 | undefined
): string | undefined {
  if (envelope?.view.type !== "qr_code") {
    return undefined;
  }
  if (envelope.view.source.type === "png_base64") {
    return `data:image/png;base64,${envelope.view.source.value}`;
  }
  try {
    const code = qrcode(0, "M");
    code.addData(envelope.view.source.value);
    code.make();
    return code.createDataURL(6, 12);
  } catch {
    return undefined;
  }
}
