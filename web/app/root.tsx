import "@fontsource-variable/manrope/wght.css";
import "@mantine/core/styles.css";
import "@mantine/notifications/styles.css";
import "./app.css";

import {
  Button,
  Center,
  ColorSchemeScript,
  MantineProvider,
  Paper,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import { Notifications } from "@mantine/notifications";
import type { ReactNode } from "react";
import {
  isRouteErrorResponse,
  Links,
  Meta,
  Outlet,
  redirect,
  Scripts,
  ScrollRestoration,
  useRouteError,
} from "react-router";

import { csrfProtectionMiddleware } from "~/api/csrf.server";
import { shouldAutomaticallyBeginOIDCLogin } from "~/api/request-auth.server";
import {
  getProductAuthentication,
  productAuthenticationMiddleware,
} from "~/features/auth/product-authentication.server";
import { ProductShell } from "~/features/shell/product-shell";
import { pageTitle } from "~/shared/page-title";
import { serverPerformanceMiddleware } from "~/features/performance/performance.server";
import { sliprampTheme } from "~/features/shell/theme";
import { ThemePresetScript } from "~/features/shell/theme-preset";

import type { Route } from "./+types/root";

export const middleware: Route.MiddlewareFunction[] = [
  csrfProtectionMiddleware,
  serverPerformanceMiddleware,
  productAuthenticationMiddleware,
];

export function meta() {
  return [{ title: pageTitle() }];
}

export function headers() {
  return {
    "Cache-Control": "private, no-store",
    "Referrer-Policy": "same-origin",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
  };
}

export async function loader({ context, request }: Route.LoaderArgs) {
  const authentication = await getProductAuthentication(context);
  if (authentication.authentication === "required") {
    if (shouldAutomaticallyBeginOIDCLogin(request)) {
      return redirect("/api/v1/auth/login");
    }
  }

  return authentication;
}

export function Layout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <meta charSet="utf-8" />
        <meta content="width=device-width, initial-scale=1" name="viewport" />
        <link href="/favicon.ico" rel="icon" sizes="any" />
        <link href="/apple-touch-icon.png" rel="apple-touch-icon" />
        <ColorSchemeScript defaultColorScheme="auto" />
        <ThemePresetScript />
        <Meta />
        <Links />
      </head>
      <body>
        <MantineProvider defaultColorScheme="auto" theme={sliprampTheme}>
          <Notifications position="top-right" />
          {children}
        </MantineProvider>
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export default function App({ loaderData }: Route.ComponentProps) {
  if (loaderData.authentication === "required") {
    return <LoginPrompt />;
  }
  if (loaderData.authentication === "unavailable") {
    return <AuthenticationUnavailable />;
  }

  return (
    <ProductShell principal={loaderData.principal}>
      <Outlet />
    </ProductShell>
  );
}

export function ErrorBoundary() {
  const error = useRouteError();
  const responseError = isRouteErrorResponse(error) ? error : undefined;
  const title = responseError
    ? `${responseError.status} ${responseError.statusText || "Request failed"}`
    : "Something went wrong";
  const message =
    responseError && typeof responseError.data === "string"
      ? responseError.data
      : "Slipramp could not render this page. Try again, or return to the target index.";

  return (
    <Center mih="100vh" p="md">
      <Paper maw={520} p="xl" shadow="sm" withBorder>
        <Stack align="flex-start" gap="md">
          <Stack gap={4}>
            <Title order={1}>{title}</Title>
            <Text c="dimmed">{message}</Text>
          </Stack>
          <Button component="a" href="/">
            Return to targets
          </Button>
        </Stack>
      </Paper>
    </Center>
  );
}

function LoginPrompt() {
  return (
    <Center mih="100vh" p="md">
      <Paper maw={420} p="xl" shadow="sm" withBorder>
        <Stack align="flex-start" gap="md">
          <Stack gap={4}>
            <Title order={1}>Sign in to Slipramp</Title>
            <Text c="dimmed">
              Continue with the identity provider configured for this installation.
            </Text>
          </Stack>
          <Button component="a" href="/api/v1/auth/login">
            Sign in
          </Button>
        </Stack>
      </Paper>
    </Center>
  );
}

function AuthenticationUnavailable() {
  return (
    <Center mih="100vh" p="md">
      <Paper maw={420} p="xl" shadow="sm" withBorder>
        <Stack gap={4}>
          <Title order={1}>Slipramp is unavailable</Title>
          <Text c="dimmed">
            The web application could not verify your session with the Slipramp Server.
          </Text>
        </Stack>
      </Paper>
    </Center>
  );
}
