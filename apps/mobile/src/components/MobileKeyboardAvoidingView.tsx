import type { PropsWithChildren } from "react";
import {
  KeyboardAvoidingView,
  type KeyboardAvoidingViewProps,
  Platform
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

export const mobileKeyboardDismissMode =
  Platform.OS === "ios" ? "interactive" : "on-drag";

export function MobileKeyboardAvoidingView({
  children,
  keyboardVerticalOffset,
  ...props
}: PropsWithChildren<Omit<KeyboardAvoidingViewProps, "behavior">>) {
  const insets = useSafeAreaInsets();

  return (
    <KeyboardAvoidingView
      {...props}
      behavior={Platform.OS === "ios" ? "padding" : "height"}
      keyboardVerticalOffset={keyboardVerticalOffset ?? insets.top}
    >
      {children}
    </KeyboardAvoidingView>
  );
}
