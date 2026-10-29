import assert from "node:assert/strict";
import test from "node:test";

import { groupTerraformResources } from "./terraform-plan-report.ts";

test("groups resource changes by module with the root module first", () => {
  const root = { action: "create", address: "aws_vpc.main", moduleAddress: undefined };
  const database = {
    action: "update",
    address: "module.database.aws_db_instance.main",
    moduleAddress: "module.database",
  };
  const network = {
    action: "replace",
    address: "module.network.aws_subnet.main",
    moduleAddress: "module.network",
  };

  assert.deepEqual(
    groupTerraformResources([network, root, database]).map((group) => ({
      addresses: group.resources.map((resource) => resource.address),
      moduleAddress: group.moduleAddress,
    })),
    [
      { addresses: ["aws_vpc.main"], moduleAddress: undefined },
      {
        addresses: ["module.database.aws_db_instance.main"],
        moduleAddress: "module.database",
      },
      {
        addresses: ["module.network.aws_subnet.main"],
        moduleAddress: "module.network",
      },
    ],
  );
});
