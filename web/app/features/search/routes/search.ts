import { data } from "react-router";
import { createServerAPIClient } from "~/api/client.server";
import { searchResources } from "~/api/generated";
import { limitSearchQuery } from "../search-state";
import type { Route } from "./+types/search";

export const shouldRevalidate = () => false;

export async function loader({ context, request }: Route.LoaderArgs) {
  const query = new URL(request.url).searchParams.get("q")?.trim() ?? "";
  const headers = { "Cache-Control": "private, no-store" };
  if (!query || limitSearchQuery(query) !== query) {
    return data({ query, results: null }, { headers, status: 400 });
  }
  const result = await searchResources({
    client: createServerAPIClient(request, context),
    query: { q: query },
    signal: request.signal,
  });
  return data(
    { query, results: result.data ?? null },
    { headers, status: result.response?.status ?? 503 },
  );
}
