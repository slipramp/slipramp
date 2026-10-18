import "@mantine/core/styles.css";

import { Anchor, AppShell, ColorSchemeScript, Group, MantineProvider } from "@mantine/core";
import type { ReactNode } from "react";
import { Link, Links, Meta, Outlet, Scripts, ScrollRestoration } from "react-router";

import { pageTitle } from "~/page-title";

export function meta() {
  return [{ title: pageTitle() }];
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

export default function App() {
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
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
    </AppShell>
  );
}
