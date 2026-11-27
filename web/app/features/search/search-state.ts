const maximumSearchLength = 200;
export const searchPaletteParam = "palette";

export function limitSearchQuery(value: string) {
  return Array.from(value).slice(0, maximumSearchLength).join("");
}

export function searchPaletteLocation(
  location: { pathname: string; search: string; hash: string },
  query: string | null,
) {
  const params = new URLSearchParams(location.search);
  if (query === null) params.delete(searchPaletteParam);
  else params.set(searchPaletteParam, limitSearchQuery(query));
  const search = params.toString();
  const pathname = location.pathname.replace(/^\/+/, "/");
  return pathname + (search ? `?${search}` : "") + location.hash;
}

export function canonicalSearchPaletteLocation(url: URL) {
  const values = url.searchParams.getAll(searchPaletteParam);
  const query = values[0];
  if (query !== undefined && (values.length > 1 || limitSearchQuery(query) !== query)) {
    return searchPaletteLocation(url, query);
  }
  return undefined;
}
