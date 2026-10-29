export const repositoryDetailListLimit = 25;

export function repositoryDetailTab(pathname: string, repositoryPath: string) {
  if (pathname === `${repositoryPath}/refresh-attempts`) return "refresh-attempts";
  if (pathname === `${repositoryPath}/commits`) return "commits";
  if (pathname === `${repositoryPath}/runs`) return "runs";
  return "overview";
}
