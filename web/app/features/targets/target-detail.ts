export function targetDetailTab(pathname: string, targetPath: string) {
  if (pathname === `${targetPath}/runs`) return "runs";
  if (pathname === `${targetPath}/defaults`) return "defaults";
  if (pathname === `${targetPath}/definition`) return "definition";
  return "overview";
}
