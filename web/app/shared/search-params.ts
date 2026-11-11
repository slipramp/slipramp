import type { ShouldRevalidateFunctionArgs } from "react-router";

const maximumListOffset = 2_147_483_647;

export function pageFromSearchParam(value: string | null, pageSize: number) {
  if (!value || !/^[1-9]\d*$/.test(value)) return 1;

  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) return 1;

  return Math.min(parsed, Math.floor(maximumListOffset / pageSize) + 1);
}

export function skipPresentationSearchParamRevalidation({
  currentUrl,
  defaultShouldRevalidate,
  formMethod,
  nextUrl,
}: ShouldRevalidateFunctionArgs) {
  const presentationNavigation =
    formMethod === undefined &&
    currentUrl.pathname === nextUrl.pathname &&
    currentUrl.search !== nextUrl.search;

  return presentationNavigation ? false : defaultShouldRevalidate;
}
