import type { ReactNode } from "react";
import {
  Pressable,
  StyleSheet,
  Text,
  View,
  type StyleProp,
  type ViewStyle
} from "react-native";
import { type NativeTheme } from "./tokens";
import { useNativeTheme } from "./theme-provider";

export interface NativeListRowProps {
  accessibilityLabel?: string;
  description?: ReactNode;
  disabled?: boolean;
  leading?: ReactNode;
  onPress?(): void;
  selected?: boolean;
  style?: StyleProp<ViewStyle>;
  title: string;
  trailing?: ReactNode;
}

export function NativeListRow({
  accessibilityLabel,
  description,
  disabled = false,
  leading,
  onPress,
  selected = false,
  style,
  title,
  trailing
}: NativeListRowProps) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const interactive = onPress !== undefined;

  return (
    <Pressable
      accessibilityLabel={accessibilityLabel ?? title}
      accessibilityRole={interactive ? "button" : undefined}
      accessibilityState={{ disabled, selected }}
      disabled={!interactive || disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.row,
        selected ? styles.selected : undefined,
        pressed && interactive && !disabled ? styles.pressed : undefined,
        disabled ? styles.disabled : undefined,
        style
      ]}
    >
      {selected ? <View style={styles.selectedIndicator} /> : null}
      {leading ? <View style={styles.leading}>{leading}</View> : null}
      <View style={styles.copy}>
        <Text
          numberOfLines={2}
          style={[styles.title, selected ? styles.titleSelected : undefined]}
        >
          {title}
        </Text>
        {typeof description === "string" ? (
          <Text numberOfLines={1} style={styles.description}>
            {description}
          </Text>
        ) : description ? (
          <View style={styles.descriptionSlot}>{description}</View>
        ) : null}
      </View>
      {trailing ? <View style={styles.trailing}>{trailing}</View> : null}
    </Pressable>
  );
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    copy: { flex: 1, paddingVertical: theme.space.small },
    description: {
      color: theme.color.muted,
      fontSize: theme.space.small,
      marginTop: theme.space.small / 2
    },
    descriptionSlot: { marginTop: theme.space.small / 2 },
    disabled: { opacity: 0.45 },
    leading: { marginRight: theme.space.small },
    pressed: { opacity: 0.7 },
    row: {
      alignItems: "center",
      borderRadius: theme.radius.small,
      flexDirection: "row",
      minHeight: theme.control.row,
      overflow: "hidden",
      paddingHorizontal: theme.space.small
    },
    selected: { backgroundColor: theme.color.panelRaised },
    selectedIndicator: {
      backgroundColor: theme.color.accent,
      borderRadius: theme.radius.small / 2,
      bottom: theme.radius.small,
      left: 0,
      position: "absolute",
      top: theme.radius.small,
      width: theme.radius.small / 2
    },
    title: {
      color: theme.color.textSecondary,
      fontSize: theme.space.medium - 2,
      fontWeight: "600",
      lineHeight: theme.space.medium + theme.space.small - 1
    },
    titleSelected: { color: theme.color.text },
    trailing: { marginLeft: theme.space.small }
  });
}
