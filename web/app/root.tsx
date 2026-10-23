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
  Scripts,
  ScrollRestoration,
  useRouteError,
} from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getCurrentPrincipal } from "~/api/generated";
import { ProductShell } from "~/components/product-shell";
import { pageTitle } from "~/page-title";
import { sliprampTheme } from "~/theme";

import type { Route } from "./+types/root";

const authenticationProxyPaths = new Set(["/api/v1/auth/login", "/api/v1/auth/callback"]);

export function meta() {
  return [{ title: pageTitle() }];
}

export function headers() {
  return {
    "Cache-Control": "no-store",
    "Referrer-Policy": "same-origin",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
  };
}

export async function loader({ request }: Route.LoaderArgs) {
  if (authenticationProxyPaths.has(new URL(request.url).pathname)) {
    return { authentication: "passthrough", principal: undefined } as const;
  }

  const result = await getCurrentPrincipal({ client: createServerAPIClient(request) });
  if (result.data) {
    return { authentication: "authenticated", principal: result.data } as const;
  }
  if (result.response?.status === 401) {
    return { authentication: "required", principal: undefined } as const;
  }
  return { authentication: "unavailable", principal: undefined } as const;
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
