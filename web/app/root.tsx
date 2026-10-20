import "@mantine/core/styles.css";

import {
  Anchor,
  AppShell,
  Badge,
  Button,
  Center,
  ColorSchemeScript,
  Group,
  MantineProvider,
  Paper,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import type { ReactNode } from "react";
import { Form, Link, Links, Meta, Outlet, Scripts, ScrollRestoration } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getCurrentPrincipal } from "~/api/generated";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/root";

const authenticationProxyPaths = new Set(["/api/v1/auth/login", "/api/v1/auth/callback"]);

export function meta() {
  return [{ title: pageTitle() }];
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
  if (result.response?.status === 503) {
    return { authentication: "disabled", principal: undefined } as const;
  }

  return { authentication: "unavailable", principal: undefined } as const;
}

export function Layout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta content="width=device-width, initial-scale=1" name="viewport" />
        <ColorSchemeScript defaultColorScheme="auto" />
        <Meta />
        <Links />
      </head>
      <body>
        <MantineProvider defaultColorScheme="auto">{children}</MantineProvider>
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

  return (
    <AppShell header={{ height: 60 }} padding="md">
      <AppShell.Header>
        <Group h="100%" justify="space-between" px="md">
          <Anchor component={Link} c="inherit" fw={700} size="lg" to="/" underline="never">
            Slipramp
          </Anchor>
          <Group gap="lg">
            <Anchor component={Link} to="/">
              Targets
            </Anchor>
            <Anchor component={Link} to="/clusters">
              Clusters
            </Anchor>
            <Anchor component={Link} to="/repositories">
              Repositories
            </Anchor>
            <Anchor component={Link} to="/runs">
              Runs
            </Anchor>
            {loaderData.principal ? (
              <Group gap="xs">
                <Text fw={500} size="sm">
                  {loaderData.principal.displayName}
                </Text>
                <Badge variant="light">{loaderData.principal.role}</Badge>
                <Form action="/logout" method="post">
                  <Button size="compact-sm" type="submit" variant="subtle">
                    Log out
                  </Button>
                </Form>
              </Group>
            ) : null}
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
    </AppShell>
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
