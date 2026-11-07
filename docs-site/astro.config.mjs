import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";

export default defineConfig({
  integrations: [
    starlight({
      customCss: ["@fontsource-variable/manrope/wght.css", "./src/styles/slipramp.css"],
      description: "The user guide for Slipramp.",
      disable404Route: true,
      editLink: {
        baseUrl: "https://github.com/slipramp/slipramp/edit/main/docs-site/",
      },
      favicon: "/favicon.ico",
      lastUpdated: true,
      pagination: true,
      sidebar: [
        {
          label: "Start here",
          items: [
            { slug: "getting-started" },
            { slug: "getting-started/installation" },
            { slug: "getting-started/quickstart" },
          ],
        },
        {
          label: "Concepts",
          items: [
            { slug: "concepts" },
            { slug: "concepts/core-model" },
            { slug: "concepts/execution" },
            { slug: "concepts/promotions" },
          ],
        },
        {
          label: "Guides",
          items: [
            { slug: "guides" },
            { slug: "guides/connect-a-repository" },
            { slug: "guides/write-a-catalog" },
            { slug: "guides/run-an-action" },
            { slug: "guides/use-artifacts" },
          ],
        },
        {
          label: "Reference",
          items: [
            { slug: "reference" },
            { slug: "reference/configuration" },
            { slug: "reference/catalog" },
            { slug: "reference/cli" },
            { slug: "reference/mcp" },
          ],
        },
        { slug: "troubleshooting" },
      ],
      social: [
        {
          href: "https://github.com/slipramp/slipramp",
          icon: "github",
          label: "GitHub",
        },
      ],
      title: "Slipramp",
    }),
  ],
  publicDir: "../web/public",
  site: "https://docs.slipramp.com",
});
