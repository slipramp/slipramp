import assert from "node:assert/strict";
import test from "node:test";

import { filterTerraformResources, groupTerraformResources } from "./terraform-plan-report.ts";

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

test("filters resource changes by no-op visibility and keywords", () => {
  const resources = [
    {
      action: "no-op",
      address: "module.network.aws_vpc.main",
      mode: "managed",
      moduleAddress: "module.network",
      name: "main",
      providerName: "registry.terraform.io/hashicorp/aws",
      type: "aws_vpc",
    },
    {
      action: "update",
      actionReason: "replace_because_cannot_update",
      address: "module.database.aws_db_instance.primary",
      mode: "managed",
      moduleAddress: "module.database",
      name: "primary",
      providerName: "registry.terraform.io/hashicorp/aws",
      type: "aws_db_instance",
    },
    {
      action: "create",
      address: "terraform_data.bootstrap",
      mode: "managed",
      name: "bootstrap",
      providerName: "terraform.io/builtin/terraform",
      type: "terraform_data",
    },
  ];

  assert.deepEqual(
    filterTerraformResources(resources, { keyword: "", showNoOp: false }).map(
      (resource) => resource.address,
    ),
    ["module.database.aws_db_instance.primary", "terraform_data.bootstrap"],
  );
  assert.deepEqual(
    filterTerraformResources(resources, { keyword: "DATABASE aws_db", showNoOp: true }).map(
      (resource) => resource.address,
    ),
    ["module.database.aws_db_instance.primary"],
  );
  assert.deepEqual(
    filterTerraformResources(resources, { keyword: "network", showNoOp: false }),
    [],
  );
  assert.deepEqual(
    filterTerraformResources(resources, { keyword: "network", showNoOp: true }).map(
      (resource) => resource.address,
    ),
    ["module.network.aws_vpc.main"],
  );
});
