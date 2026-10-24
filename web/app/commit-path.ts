export function commitPath(repositoryId: string, commitSha: string) {
  return `/repositories/${repositoryId}/commits/${commitSha}`;
}
