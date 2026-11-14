export const placementDetailRunLimit = 25;

export function placementDetailTab(pathname: string, placementPath: string) {
  if (pathname === `${placementPath}/preflight`) return "preflight";
  if (pathname === `${placementPath}/runs`) return "runs";
  if (pathname === `${placementPath}/definition`) return "definition";
  return "overview";
}
