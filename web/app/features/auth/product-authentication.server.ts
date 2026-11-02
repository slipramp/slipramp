import { createContext, type MiddlewareFunction, type RouterContextProvider } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { getCurrentPrincipal, type ProductPrincipal } from "~/api/generated";

const authenticationProxyPaths = new Set(["/api/v1/auth/login", "/api/v1/auth/callback"]);

export type ProductAuthentication =
  | { authentication: "authenticated"; principal: ProductPrincipal }
  | { authentication: "passthrough"; principal: undefined }
  | { authentication: "required"; principal: undefined }
  | { authentication: "unavailable"; principal: undefined };

type ProductAuthenticationResolver = () => Promise<ProductAuthentication>;

const productAuthenticationResolverContext = createContext<
  ProductAuthenticationResolver | undefined
>(undefined);

export const productAuthenticationMiddleware: MiddlewareFunction<Response> = async (
  { context, request },
  next,
) => {
  let authentication: Promise<ProductAuthentication> | undefined;
  context.set(productAuthenticationResolverContext, () => {
    authentication ??= loadProductAuthentication(request, context);
    return authentication;
  });

  return next();
};

export function getProductAuthentication(context: Pick<RouterContextProvider, "get">) {
  const resolve = context.get(productAuthenticationResolverContext);
  if (!resolve) throw new Error("Product authentication middleware did not run");

  return resolve();
}

export async function getProductPrincipal(context: Pick<RouterContextProvider, "get">) {
  return (await getProductAuthentication(context)).principal;
}

async function loadProductAuthentication(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
): Promise<ProductAuthentication> {
  if (authenticationProxyPaths.has(new URL(request.url).pathname)) {
    return { authentication: "passthrough", principal: undefined };
  }

  const result = await getCurrentPrincipal({ client: createServerAPIClient(request, context) });
  if (result.data) {
    return { authentication: "authenticated", principal: result.data };
  }
  if (result.response?.status === 401) {
    return { authentication: "required", principal: undefined };
  }
  return { authentication: "unavailable", principal: undefined };
}
