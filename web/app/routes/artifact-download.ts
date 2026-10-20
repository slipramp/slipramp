import { proxyArtifactDownload } from "~/api/artifact-download.server";
import { getServerAPIURL } from "~/api/client.server";

import type { Route } from "./+types/artifact-download";

export async function loader({ params, request }: Route.LoaderArgs) {
  return proxyArtifactDownload(params.artifactId, request, getServerAPIURL());
}
