import type { InboxDestination } from "~/api/generated";

const problemIDPattern = /^[0-9a-f]{64}$/;
const maximumAcknowledgementBatch = 500;

export function inboxDestinationHref(destination: InboxDestination) {
  switch (destination.type) {
    case "promotion":
      return `/promotions/${destination.id}`;
    case "repository":
      return `/repositories/${destination.id}`;
    case "cluster":
      return `/clusters/${encodeURIComponent(destination.id)}`;
    case "run":
      return `/runs/${destination.id}`;
    default:
      return unreachableDestination(destination.type);
  }
}

function unreachableDestination(value: never): never {
  throw new Error(`Unsupported Inbox destination: ${String(value)}`);
}

export function acknowledgementProblemIDs(formData: FormData) {
  const values = formData.getAll("problemId");
  if (values.length === 0 || values.length > maximumAcknowledgementBatch) {
    return undefined;
  }

  const problemIDs: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    if (typeof value !== "string" || !problemIDPattern.test(value) || seen.has(value)) {
      return undefined;
    }
    problemIDs.push(value);
    seen.add(value);
  }

  return problemIDs;
}
