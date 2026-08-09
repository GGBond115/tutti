import type { ReactNode } from "react";
import { View } from "react-native";

interface MobileControlGlyphProps {
  color: string;
  size?: number;
}

export function MobileAddGlyph({ color, size = 20 }: MobileControlGlyphProps) {
  const stroke = Math.max(2, size * 0.1);
  return (
    <GlyphFrame size={size}>
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: size * 0.2,
          position: "absolute",
          top: (size - stroke) / 2,
          width: size * 0.6
        }}
      />
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: size * 0.6,
          left: (size - stroke) / 2,
          position: "absolute",
          top: size * 0.2,
          width: stroke
        }}
      />
    </GlyphFrame>
  );
}

export function MobileBackGlyph({ color, size = 20 }: MobileControlGlyphProps) {
  const stroke = Math.max(2, size * 0.1);
  const wing = size * 0.38;
  return (
    <GlyphFrame size={size}>
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: size * 0.16,
          position: "absolute",
          top: (size - stroke) / 2,
          width: size * 0.68
        }}
      />
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: size * 0.14,
          position: "absolute",
          top: size * 0.34,
          transform: [{ rotate: "-45deg" }],
          width: wing
        }}
      />
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: size * 0.14,
          position: "absolute",
          top: size * 0.61,
          transform: [{ rotate: "45deg" }],
          width: wing
        }}
      />
    </GlyphFrame>
  );
}

export function MobileChevronGlyph({
  color,
  direction,
  size = 18
}: MobileControlGlyphProps & {
  direction: "down" | "left" | "right" | "up";
}) {
  const stroke = Math.max(2, size * 0.11);
  const bar = size * 0.48;
  const transforms =
    direction === "down"
      ? (["45deg", "-45deg"] as const)
      : direction === "up"
        ? (["-45deg", "45deg"] as const)
        : direction === "right"
          ? (["45deg", "-45deg"] as const)
          : (["-45deg", "45deg"] as const);
  const vertical = direction === "left" || direction === "right";

  return (
    <GlyphFrame size={size}>
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: vertical ? size * 0.28 : size * 0.18,
          position: "absolute",
          top: vertical ? size * 0.34 : size * 0.42,
          transform: [{ rotate: transforms[0] }],
          width: bar
        }}
      />
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: vertical ? size * 0.28 : size * 0.48,
          position: "absolute",
          top: vertical ? size * 0.6 : size * 0.42,
          transform: [{ rotate: transforms[1] }],
          width: bar
        }}
      />
    </GlyphFrame>
  );
}

export function MobileSendGlyph({ color, size = 20 }: MobileControlGlyphProps) {
  const stroke = Math.max(2, size * 0.1);
  return (
    <GlyphFrame size={size}>
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: size * 0.68,
          left: (size - stroke) / 2,
          position: "absolute",
          top: size * 0.2,
          width: stroke
        }}
      />
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: size * 0.28,
          position: "absolute",
          top: size * 0.26,
          transform: [{ rotate: "-45deg" }],
          width: size * 0.38
        }}
      />
      <View
        style={{
          backgroundColor: color,
          borderRadius: stroke / 2,
          height: stroke,
          left: size * 0.46,
          position: "absolute",
          top: size * 0.26,
          transform: [{ rotate: "45deg" }],
          width: size * 0.38
        }}
      />
    </GlyphFrame>
  );
}

export function MobileStopGlyph({ color, size = 20 }: MobileControlGlyphProps) {
  const square = size * 0.48;
  return (
    <GlyphFrame size={size}>
      <View
        style={{
          backgroundColor: color,
          borderRadius: Math.max(2, size * 0.08),
          height: square,
          left: (size - square) / 2,
          position: "absolute",
          top: (size - square) / 2,
          width: square
        }}
      />
    </GlyphFrame>
  );
}

export function MobileStatusDotGlyph({
  color,
  size = 8
}: MobileControlGlyphProps) {
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={{
        backgroundColor: color,
        borderRadius: size / 2,
        height: size,
        width: size
      }}
    />
  );
}

function GlyphFrame({ children, size }: { children: ReactNode; size: number }) {
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={{ height: size, width: size }}
    >
      {children}
    </View>
  );
}
