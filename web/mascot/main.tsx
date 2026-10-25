import "@fontsource-variable/manrope/wght.css";
import "@mantine/core/styles.css";
import "../app/app.css";
import "./site.css";

import {
  Anchor,
  Badge,
  Box,
  Container,
  Group,
  Image,
  MantineProvider,
  Paper,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import { IconBrandGithub, IconSparkles } from "@tabler/icons-react";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import mascotUrl from "~/assets/slipramp-mascot.webp";
import { MascotGallery, mascotPoseCount } from "~/components/mascot-gallery";
import { sliprampTheme } from "~/theme";

document.documentElement.dataset.sliprampTheme = "kawaii";

function MascotSite() {
  return (
    <Box className="mascot-site">
      <Box className="mascot-site-nav" component="header">
        <Container size="xl">
          <Group h={64} justify="space-between" wrap="nowrap">
            <Group gap="sm" wrap="nowrap">
              <Image alt="" aria-hidden="true" h={34} src="/slipramp-mark.png" w={34} />
              <Text className="mascot-site-brand" fw={750} size="lg">
                Slipramp
              </Text>
            </Group>
            <Anchor
              className="mascot-site-github"
              href="https://github.com/slipramp/slipramp"
              rel="noreferrer"
              target="_blank"
              underline="never"
            >
              <Group gap={6} wrap="nowrap">
                <IconBrandGithub aria-hidden="true" size={19} stroke={1.8} />
                <Text component="span" fw={650} visibleFrom="xs">
                  View on GitHub
                </Text>
              </Group>
            </Anchor>
          </Group>
        </Container>
      </Box>

      <Container className="mascot-site-content" component="main" size="xl">
        <Paper className="mascot-site-hero" p={{ base: "xl", md: 44 }} withBorder>
          <Group align="center" gap="xl" justify="space-between">
            <Stack className="mascot-site-hero-copy" gap="md">
              <Badge
                color="pink"
                leftSection={<IconSparkles aria-hidden="true" size={14} stroke={2} />}
                size="lg"
                variant="light"
              >
                {mascotPoseCount} production loops
              </Badge>
              <Stack gap="xs">
                <Title order={1}>One axolotl. Every operational mood.</Title>
                <Text c="dimmed" maw={660} size="lg">
                  Meet the animated teammate who waits, works, celebrates, and weathers the
                  occasional failure alongside Slipramp Runs and Promotions.
                </Text>
              </Stack>
            </Stack>
            <Image
              alt="Slipramp's pink axolotl mascot"
              className="mascot-site-hero-image"
              decoding="async"
              src={mascotUrl}
            />
          </Group>
        </Paper>

        <MascotGallery />
      </Container>

      <Box className="mascot-site-footer" component="footer">
        <Container size="xl">
          <Group gap="xs" justify="space-between">
            <Text c="dimmed" size="sm">
              Built for Slipramp, a GitOps-driven infrastructure control plane.
            </Text>
            <Text c="dimmed" size="sm">
              Activity respects reduced-motion preferences.
            </Text>
          </Group>
        </Container>
      </Box>
    </Box>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <MantineProvider defaultColorScheme="light" forceColorScheme="light" theme={sliprampTheme}>
      <MascotSite />
    </MantineProvider>
  </StrictMode>,
);
