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
  IconGitCommit,
  IconHistory,
  IconInbox,
  IconKey,
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
  IconLogout,
  IconMoon,
  IconNotification,
  IconPalette,
  IconRobot,
  IconRocket,
  IconRoute,
  IconServer2,
  IconShieldCheck,
  IconSparkles,
  IconSun,
  IconTargetArrow,
  type TablerIcon,
} from "@tabler/icons-react";
import { useEffect, useState, type ReactNode } from "react";
import { Form, Link, useLocation, useNavigation } from "react-router";

import type { ProductPrincipal } from "~/api/generated";
import mascotUrl from "~/assets/slipramp-mascot.webp";
import { ActivityRefreshControl, ActivityRefreshProvider } from "~/components/activity-refresh";
import { Identity } from "~/components/identity";
import { PerformanceFooter } from "~/components/performance-footer";
import { useThemePreset, type ThemePreset } from "~/theme-preset";

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
      { icon: IconGitCommit, label: "Commits", to: "/commits" },
      { icon: IconNotification, label: "Notifications", to: "/notification-deliveries" },
    ],
    label: "Operate",
  },
  {
    items: [
      { icon: IconFolder, label: "Projects", to: "/promotion-projects" },
      { icon: IconRocket, label: "Promotions", to: "/promotions" },
      { icon: IconRoute, label: "Pipelines", to: "/promotion-pipelines?status=different" },
      {
        icon: IconArrowsDiff,
        label: "Branch drift",
        to: "/promotion-branch-drift?status=drifted",
      },
      { icon: IconShieldCheck, label: "Stage policies", to: "/promotion-stages" },
    ],
    label: "Promote",
  },
  {
    items: [
      { icon: IconGitBranch, label: "Repositories", to: "/repositories" },
      { icon: IconServer2, label: "Clusters", to: "/clusters" },
    ],
    label: "Inventory",
  },
];

export function ProductShell({
  children,
  principal,
}: Readonly<{ children: ReactNode; principal?: ProductPrincipal }>) {
  return (
    <ActivityRefreshProvider>
      <ProductShellLayout principal={principal}>{children}</ProductShellLayout>
    </ActivityRefreshProvider>
  );
}

function ProductShellLayout({
  children,
  principal,
}: Readonly<{ children: ReactNode; principal?: ProductPrincipal }>) {
  const [mobileOpened, setMobileOpened] = useState(false);
  const [desktopOpened, setDesktopOpened] = useState(true);
  const navigation = useNavigation();
  const loading = navigation.state !== "idle";

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
      <Box
        aria-hidden={!loading}
        aria-label="Loading page"
        className="global-loading-bar"
        component="output"
        data-active={loading || undefined}
      >
        <Box component="span" />
      </Box>

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
          <Group className="product-header-controls" gap="xs" h="100%" wrap="nowrap">
            <Box aria-hidden="true" className="product-header-mascot-slot">
              <img alt="" className="product-header-mascot" decoding="async" src={mascotUrl} />
            </Box>
            <ActivityRefreshControl />
            <AppearanceControl />
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar className="product-navbar" p="sm">
        <AppShell.Section component={ScrollArea} grow>
          <Stack gap="md">
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
            <Divider mb="sm" />
            <Stack gap="xs">
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

      <AppShell.Main className="product-main">
        <Box className="page-surface">
          <Box className="page-content">{children}</Box>
          <PerformanceFooter />
        </Box>
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
    <Stack gap={4}>
      <Text c="dimmed" className="navigation-label" fw={700} size="xs" tt="uppercase">
        {group.label}
      </Text>
      {group.items.map((item) => {
        const ItemIcon = item.icon;
        const queryIndex = item.to.indexOf("?");
        const itemPathname = queryIndex === -1 ? item.to : item.to.slice(0, queryIndex);
        const active =
          itemPathname === "/"
            ? location.pathname === "/"
            : location.pathname === itemPathname ||
              location.pathname.startsWith(`${itemPathname}/`);

        return (
          <NavLink
            active={active}
            className="navigation-link"
            component={Link}
            key={item.to}
            label={item.label}
            leftSection={
              <ThemeIcon size={28} variant={active ? "filled" : "light"}>
                <ItemIcon size={17} stroke={1.9} />
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

function AppearanceControl() {
  const computedColorScheme = useComputedColorScheme("light");
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const { setThemePreset, themePreset } = useThemePreset();

  useEffect(() => {
    if (themePreset === "kawaii" && colorScheme !== "light") {
      setColorScheme("light");
    }
  }, [colorScheme, setColorScheme, themePreset]);

  function selectThemePreset(preset: ThemePreset) {
    setThemePreset(preset);
    if (preset === "kawaii") {
      setColorScheme("light");
    }
  }

  return (
    <Menu position="bottom-end" shadow="md" width={200}>
      <Menu.Target>
        <ActionIcon
          aria-label="Choose theme and appearance"
          size="lg"
          title="Choose theme and appearance"
          variant="light"
        >
          {themePreset === "kawaii" ? (
            <IconSparkles size={19} stroke={1.8} />
          ) : computedColorScheme === "dark" ? (
            <IconMoon size={19} stroke={1.8} />
          ) : (
            <IconSun size={19} stroke={1.8} />
          )}
        </ActionIcon>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Label>Theme</Menu.Label>
        {(
          [
            { icon: IconPalette, label: "Standard", value: "standard" },
            { icon: IconSparkles, label: "Kawaii", value: "kawaii" },
          ] as const
        ).map((preset) => {
          const PresetIcon = preset.icon;
          return (
            <Menu.Item
              key={preset.value}
              leftSection={<PresetIcon size={16} stroke={1.8} />}
              onClick={() => selectThemePreset(preset.value)}
              rightSection={
                themePreset === preset.value ? <IconCheck size={15} stroke={2} /> : null
              }
            >
              {preset.label}
            </Menu.Item>
          );
        })}
        <Menu.Divider />
        <Menu.Label>
          {themePreset === "kawaii" ? "Appearance · Kawaii is light" : "Appearance"}
        </Menu.Label>
        {(["auto", "light", "dark"] as const).map((scheme) => {
          const SchemeIcon =
            scheme === "auto" ? IconDeviceDesktop : scheme === "light" ? IconSun : IconMoon;

          return (
            <Menu.Item
              disabled={themePreset === "kawaii" && scheme !== "light"}
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
