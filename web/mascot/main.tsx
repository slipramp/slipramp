import "@fontsource-variable/manrope/wght.css";
import "@mantine/core/styles.css";
import "../app/app.css";
import "../shared/site-shell.css";
import "./site.css";

import {
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
import { IconSparkles } from "@tabler/icons-react";
import { StrictMode, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes, useParams } from "react-router";

import mascotUrl from "~/features/mascot/assets/slipramp-mascot.webp";
import { MascotGallery, MascotPoseDetail, mascotPoseCount } from "~/features/mascot/mascot-gallery";
import { findMascotPose } from "~/features/mascot/mascot-poses";
import { sliprampTheme } from "~/features/shell/theme";

document.documentElement.dataset.sliprampTheme = "kawaii";

function MascotSite() {
  return (
    <Box className="mascot-site">
      <header className="site-header">
        <div className="shell nav-shell">
          <a aria-label="Slipramp home" className="brand" href="https://slipramp.com/">
            <img alt="" aria-hidden="true" height="36" src="/slipramp-mark.png" width="36" />
            <span>Slipramp</span>
          </a>
          <nav aria-label="Primary navigation">
            <a href="https://slipramp.com/#how-it-works">How it works</a>
            <a href="https://slipramp.com/privacy/">Privacy</a>
            <a aria-current="page" href="/">
              Mascot
            </a>
            <a className="nav-github" href="https://github.com/slipramp/slipramp">
              GitHub
            </a>
          </nav>
        </div>
      </header>

      <Container className="mascot-site-content" component="main" size="xl">
        <Routes>
          <Route element={<MascotGalleryPage />} path="/" />
          <Route element={<MascotPosePage />} path="/poses/:pose" />
          <Route element={<MascotPosePage />} path="*" />
        </Routes>
      </Container>

      <footer className="site-footer">
        <div className="shell footer-shell">
          <a aria-label="Slipramp home" className="brand" href="https://slipramp.com/">
            <img alt="" aria-hidden="true" height="30" src="/slipramp-mark.png" width="30" />
            <span>Slipramp</span>
          </a>
          <p>GitOps-driven infrastructure control for homelabs and teams.</p>
          <nav aria-label="Footer navigation">
            <a href="https://github.com/slipramp/slipramp">GitHub</a>
            <a aria-current="page" href="/">
              Mascot
            </a>
            <a href="https://slipramp.com/privacy/">Privacy</a>
          </nav>
        </div>
      </footer>
    </Box>
  );
}

function MascotGalleryPage() {
  useDocumentTitle("Slipramp mascot gallery");

  return (
    <>
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
                Meet the animated teammate who waits, works, celebrates, and weathers the occasional
                failure alongside Slipramp Runs and Promotions.
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

      <MascotGallery detailPathPrefix="/poses" galleryPath="/" />
    </>
  );
}

function MascotPosePage() {
  const { pose: poseName } = useParams();
  const pose = findMascotPose(poseName);
  useDocumentTitle(
    pose ? `${pose.label} · Slipramp mascot gallery` : "Mascot pose not found · Slipramp",
  );

  return <MascotPoseDetail galleryPath="/" pose={poseName} />;
}

function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = title;
  }, [title]);
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <MantineProvider defaultColorScheme="light" forceColorScheme="light" theme={sliprampTheme}>
      <BrowserRouter>
        <MascotSite />
      </BrowserRouter>
    </MantineProvider>
  </StrictMode>,
);
