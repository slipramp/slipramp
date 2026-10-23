import {
  ActionIcon,
  AppShell,
  Badge,
  Box,
  Burger,
  Button,
  Divider,
  Group,
  Menu,
  NavLink,
  ScrollArea,
  Stack,
  Text,
  ThemeIcon,
  Tooltip,
  useComputedColorScheme,
  useMantineColorScheme,
} from "@mantine/core";
import {
  IconArrowsDiff,
  IconCheck,
  IconDeviceDesktop,
  IconFolder,
  IconGitBranch,
  IconHistory,
  IconInbox,
  IconKey,
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
  IconLogout,
  IconMoon,
  IconRobot,
  IconRocket,
  IconRoute,
  IconServer2,
  IconShieldCheck,
  IconSun,
  IconTargetArrow,
  type TablerIcon,
} from "@tabler/icons-react";
import { useState, type ReactNode } from "react";
import { Form, Link, useLocation } from "react-router";

import type { ProductPrincipal } from "~/api/generated";
import { Identity } from "~/components/identity";

type NavigationItem = {
  icon: TablerIcon;
  label: string;
  to: string;
};

const navigationGroups: Array<{ label: string; items: NavigationItem[] }> = [
  {
    items: [
      { icon: IconInbox, label: "Inbox", to: "/" },
      { icon: IconTargetArrow, label: "Targets", to: "/targets" },
      { icon: IconHistory, label: "Runs", to: "/runs" },
    ],
    label: "Operate",
  },
  {
    items: [
      { icon: IconGitBranch, label: "Repositories", to: "/repositories" },
      { icon: IconServer2, label: "Clusters", to: "/clusters" },
    ],
    label: "Inventory",
  },
  {
    items: [
      { icon: IconFolder, label: "Projects", to: "/promotion-projects" },
      { icon: IconRocket, label: "Promotions", to: "/promotions" },
      { icon: IconRoute, label: "Pipelines", to: "/promotion-pipelines" },
      { icon: IconArrowsDiff, label: "Branch drift", to: "/promotion-branch-drift" },
      { icon: IconShieldCheck, label: "Stage policies", to: "/promotion-stages" },
    ],
    label: "Promote",
  },
];

export function ProductShell({
  children,
  principal,
}: Readonly<{ children: ReactNode; principal?: ProductPrincipal }>) {
  const [mobileOpened, setMobileOpened] = useState(false);
  const [desktopOpened, setDesktopOpened] = useState(true);

  function toggleDesktopNavigation() {
    setDesktopOpened((current) => !current);
  }

  return (
    <AppShell
      className="product-shell"
      header={{ height: 64 }}
      navbar={{
        breakpoint: "md",
        collapsed: { desktop: !desktopOpened, mobile: !mobileOpened },
        width: 272,
      }}
      padding={{ base: "md", sm: "xl" }}
    >
      <AppShell.Header className="product-header">
        <Group h="100%" justify="space-between" px={{ base: "md", sm: "xl" }}>
          <Group gap="sm">
            <Burger
              aria-label={mobileOpened ? "Close navigation" : "Open navigation"}
              hiddenFrom="md"
              onClick={() => setMobileOpened((current) => !current)}
              opened={mobileOpened}
              size="sm"
            />
            <Tooltip label={desktopOpened ? "Unpin navigation" : "Pin navigation"}>
              <ActionIcon
                aria-label={desktopOpened ? "Unpin navigation" : "Pin navigation"}
                onClick={toggleDesktopNavigation}
                variant={desktopOpened ? "light" : "subtle"}
                visibleFrom="md"
              >
                {desktopOpened ? (
                  <IconLayoutSidebarLeftCollapse size={19} stroke={1.8} />
                ) : (
                  <IconLayoutSidebarLeftExpand size={19} stroke={1.8} />
                )}
              </ActionIcon>
            </Tooltip>
            <Link className="product-brand" to="/">
              <img alt="" aria-hidden="true" className="product-mark" src="/slipramp-mark.png" />
              <span>Slipramp</span>
            </Link>
            <Text c="dimmed" className="product-tagline" size="sm" visibleFrom="sm">
              Infrastructure control plane
            </Text>
          </Group>
          <ColorSchemeControl />
        </Group>
      </AppShell.Header>

      <AppShell.Navbar className="product-navbar" p="md">
        <AppShell.Section component={ScrollArea} grow>
          <Stack gap="lg">
            {navigationGroups.map((group) => (
              <NavigationGroup
                closeMobile={() => setMobileOpened(false)}
                group={group}
                key={group.label}
              />
            ))}
            {principal ? (
              <NavigationGroup
                closeMobile={() => setMobileOpened(false)}
                group={{
                  items: [
                    { icon: IconKey, label: "Personal API keys", to: "/access" },
                    ...(principal.role === "admin"
                      ? [
                          {
                            icon: IconRobot,
                            label: "Service accounts",
                            to: "/service-accounts",
                          },
                        ]
                      : []),
                  ],
                  label: "Administer",
                }}
              />
            ) : null}
          </Stack>
        </AppShell.Section>

        {principal ? (
          <AppShell.Section>
            <Divider mb="md" />
            <Stack gap="sm">
              <Group justify="space-between" wrap="nowrap">
                <Identity
                  avatarSize={30}
                  detail={
                    <Text c="dimmed" size="xs">
                      Signed in
                    </Text>
                  }
                  name={principal.displayName}
                />
                <Badge variant="light">{principal.role}</Badge>
              </Group>
              <Form action="/logout" method="post">
                <Button
                  fullWidth
                  leftSection={<IconLogout size={16} stroke={1.8} />}
                  size="compact-sm"
                  type="submit"
                  variant="subtle"
                >
                  Log out
                </Button>
              </Form>
            </Stack>
          </AppShell.Section>
        ) : null}
      </AppShell.Navbar>

      <AppShell.Main>
        <Box className="page-surface">{children}</Box>
      </AppShell.Main>
    </AppShell>
  );
}

function NavigationGroup({
  closeMobile,
  group,
}: Readonly<{
  closeMobile: () => void;
  group: { label: string; items: NavigationItem[] };
}>) {
  const location = useLocation();

  return (
    <Stack gap={6}>
      <Text c="dimmed" className="navigation-label" fw={700} size="xs" tt="uppercase">
        {group.label}
      </Text>
      {group.items.map((item) => {
        const ItemIcon = item.icon;
        const active =
          item.to === "/"
            ? location.pathname === "/"
            : location.pathname === item.to || location.pathname.startsWith(`${item.to}/`);

        return (
          <NavLink
            active={active}
            className="navigation-link"
            component={Link}
            key={item.to}
            label={item.label}
            leftSection={
              <ThemeIcon size={30} variant={active ? "filled" : "light"}>
                <ItemIcon size={18} stroke={1.9} />
              </ThemeIcon>
            }
            onClick={closeMobile}
            to={item.to}
            variant="light"
          />
        );
      })}
    </Stack>
  );
}

function ColorSchemeControl() {
  const computedColorScheme = useComputedColorScheme("light");
  const { colorScheme, setColorScheme } = useMantineColorScheme();

  return (
    <Menu position="bottom-end" shadow="md" width={160}>
      <Menu.Target>
        <ActionIcon
          aria-label="Choose color scheme"
          size="lg"
          title="Choose color scheme"
          variant="light"
        >
          {computedColorScheme === "dark" ? (
            <IconMoon size={19} stroke={1.8} />
          ) : (
            <IconSun size={19} stroke={1.8} />
          )}
        </ActionIcon>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Label>Appearance</Menu.Label>
        {(["auto", "light", "dark"] as const).map((scheme) => {
          const SchemeIcon =
            scheme === "auto" ? IconDeviceDesktop : scheme === "light" ? IconSun : IconMoon;

          return (
            <Menu.Item
              key={scheme}
              leftSection={<SchemeIcon size={16} stroke={1.8} />}
              onClick={() => setColorScheme(scheme)}
              rightSection={colorScheme === scheme ? <IconCheck size={15} stroke={2} /> : null}
            >
              {scheme === "auto" ? "System" : scheme[0]?.toUpperCase() + scheme.slice(1)}
            </Menu.Item>
          );
        })}
      </Menu.Dropdown>
    </Menu>
  );
}
