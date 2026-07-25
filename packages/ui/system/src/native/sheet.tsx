import {
  BottomSheetModal,
  type BottomSheetModalProps
} from "@gorhom/bottom-sheet";
import { useEffect, useRef, type ReactNode } from "react";
import { useNativeTheme } from "./theme-provider";

export interface NativeSheetProps {
  children: ReactNode;
  onOpenChange(open: boolean): void;
  open: boolean;
  snapPoints?: BottomSheetModalProps["snapPoints"];
}

/**
 * Controlled modal sheet backed by @gorhom/bottom-sheet.
 *
 * The app root owns BottomSheetModalProvider; callers own whether the sheet is
 * open and what product actions its content performs.
 */
export function NativeSheet({
  children,
  onOpenChange,
  open,
  snapPoints
}: NativeSheetProps) {
  const theme = useNativeTheme();
  const sheet = useRef<BottomSheetModal>(null);

  useEffect(() => {
    if (open) {
      sheet.current?.present();
      return;
    }

    sheet.current?.dismiss();
  }, [open]);

  return (
    <BottomSheetModal
      backgroundStyle={{
        backgroundColor: theme.color.panelRaised,
        borderTopLeftRadius: theme.radius.large,
        borderTopRightRadius: theme.radius.large
      }}
      enableDynamicSizing={snapPoints === undefined}
      handleIndicatorStyle={{ backgroundColor: theme.color.muted }}
      onDismiss={() => onOpenChange(false)}
      ref={sheet}
      snapPoints={snapPoints}
    >
      {children}
    </BottomSheetModal>
  );
}
