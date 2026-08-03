import { Image, StyleSheet, Text, View } from "react-native";
import { type NativeTheme, useNativeTheme } from "@tutti-os/ui-system/native";
import tuttiMark from "../assets/tutti-mark.png";
import { t } from "../i18n";
import type { LoginSnapshot } from "../services/loginService";
import { PrimaryButton } from "../components/PrimaryButton";

interface LoginScreenViewProps {
  model: LoginSnapshot;
  onLogin(): void;
}

export function LoginScreenView({ model, onLogin }: LoginScreenViewProps) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);

  return (
    <View style={styles.root}>
      <View style={styles.brand}>
        <Image
          accessibilityLabel={t("appName")}
          source={tuttiMark}
          style={styles.mark}
        />
        <Text style={styles.appName}>{t("appName")}</Text>
        <Text style={styles.subtitle}>{t("loginSubtitle")}</Text>
      </View>
      <View style={styles.footer}>
        {model.errorCode === "request_failed" ? (
          <Text style={styles.error}>{t("genericError")}</Text>
        ) : null}
        <PrimaryButton
          disabled={model.pending}
          label={t("loginAction")}
          loading={model.pending}
          onPress={onLogin}
        />
      </View>
    </View>
  );
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    appName: {
      color: theme.color.text,
      fontSize: 28,
      fontWeight: "700",
      letterSpacing: -0.5,
      marginTop: theme.space.medium
    },
    brand: {
      alignItems: "center",
      flex: 1,
      justifyContent: "center"
    },
    error: {
      color: theme.color.danger,
      fontSize: 14,
      lineHeight: 20,
      textAlign: "center"
    },
    footer: {
      gap: theme.space.small,
      paddingBottom: theme.space.large,
      paddingHorizontal: theme.space.large
    },
    mark: {
      borderRadius: 24,
      height: 96,
      width: 96
    },
    root: {
      backgroundColor: theme.color.background,
      flex: 1
    },
    subtitle: {
      color: theme.color.textSecondary,
      fontSize: 15,
      lineHeight: 23,
      marginTop: theme.space.small,
      textAlign: "center",
      maxWidth: 420
    }
  });
}
