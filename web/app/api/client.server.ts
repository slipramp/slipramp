import { createClient } from "./generated/client";

const defaultAPIURL = "http://127.0.0.1:8080";

export function getServerAPIURL() {
  return process.env.SLIPRAMP_API_URL ?? defaultAPIURL;
}

export function createServerAPIClient() {
  return createClient({
    baseUrl: getServerAPIURL(),
  });
}
