import "@mantine/core/styles.css";

import { Anchor, AppShell, ColorSchemeScript, Group, MantineProvider, Text } from "@mantine/core";
import type { ReactNode } from "react";
import { Link, Links, Meta, Outlet, Scripts, ScrollRestoration } from "react-router";

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
          <Text fw={700}>Slipramp</Text>
          <Group gap="lg">
            <Anchor component={Link} to="/">
              Overview
            </Anchor>
            <Anchor component={Link} to="/repositories">
              Repositories
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
