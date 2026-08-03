import { BottomSheetModalProvider } from "@gorhom/bottom-sheet";
import { PortalHost } from "@rn-primitives/portal";
import { NativeThemeProvider } from "@tutti-os/ui-system/native";
import type { PropsWithChildren } from "react";
import { StyleSheet } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { useServiceSnapshot } from "../bindings/useServiceSnapshot";
import { mobileThemePreferenceService } from "../mobileRuntime";

export function MobileUIProviders({ children }: PropsWithChildren) {
  const { preference } = useServiceSnapshot(mobileThemePreferenceService);
  return (
    <GestureHandlerRootView style={styles.root}>
      <SafeAreaProvider>
        <NativeThemeProvider mode={preference}>
          <BottomSheetModalProvider>
            {children}
            <PortalHost />
          </BottomSheetModalProvider>
        </NativeThemeProvider>
      </SafeAreaProvider>
    </GestureHandlerRootView>
  );
}

const styles = StyleSheet.create({ root: { flex: 1 } });
