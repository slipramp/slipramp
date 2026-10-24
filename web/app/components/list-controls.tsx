import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Group,
  Menu,
  NativeSelect,
  Pagination,
  Paper,
  Stack,
  Text,
  TextInput,
} from "@mantine/core";
import { useDebouncedCallback } from "@mantine/hooks";
import { IconFilter, IconPlus, IconX } from "@tabler/icons-react";
import { type FormEvent, useState } from "react";
import { Form, Link, useSearchParams, useSubmit } from "react-router";

const filterDebounceMs = 250;

type SearchParamFilterBase = {
  active: boolean;
  disabled?: boolean;
  label: string;
  name: string;
  value?: string;
};

type SearchParamTextFilter = SearchParamFilterBase & {
  kind: "text";
  maxLength?: number;
  placeholder?: string;
};

type SearchParamSelectFilter = SearchParamFilterBase & {
  clearWhenEmpty?: string[];
  data: Array<{ label: string; value: string }>;
  kind: "select";
};

export type SearchParamFilter = SearchParamSelectFilter | SearchParamTextFilter;

export function SearchParamFilters({
  clearTo,
  filters,
}: Readonly<{
  clearTo: string;
  filters: SearchParamFilter[];
}>) {
  const [searchParams] = useSearchParams();
  const submit = useSubmit();
  const submitTextFilters = useDebouncedCallback(submitFilters, filterDebounceMs);
  const activeNames = filters.filter((filter) => filter.active).map((filter) => filter.name);
  const [addedNames, setAddedNames] = useState<string[]>([]);
  const visibleNames = [...new Set([...activeNames, ...addedNames])];

  function handleChange(event: FormEvent<HTMLFormElement>) {
    const form = event.currentTarget;
    const target = event.target;

    if (target instanceof HTMLInputElement && ["search", "text"].includes(target.type)) {
      submitTextFilters(form);
      return;
    }

    submitTextFilters.cancel();
    submitFilters(form);
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    submitTextFilters.cancel();
    submitFilters(event.currentTarget);
  }

  function submitFilters(form: HTMLFormElement) {
    const nextSearchParams = new URLSearchParams();
    for (const [name, value] of new FormData(form)) {
      if (typeof value === "string" && value.trim() !== "") {
        nextSearchParams.append(name, value.trim());
      }
    }
    submit(nextSearchParams, { method: "get", replace: true });
  }

  function removeFilter(filter: SearchParamFilter) {
    submitTextFilters.cancel();
    const dependentNames = filter.kind === "select" ? (filter.clearWhenEmpty ?? []) : [];
    setAddedNames((current) =>
      current.filter((name) => name !== filter.name && !dependentNames.includes(name)),
    );

    if (!filter.active) return;

    const nextSearchParams = new URLSearchParams(searchParams);
    nextSearchParams.delete(filter.name);
    for (const name of dependentNames) nextSearchParams.delete(name);
    nextSearchParams.delete("page");
    submit(nextSearchParams, { method: "get", replace: true });
  }

  const visibleFilters = visibleNames
    .map((name) => filters.find((filter) => filter.name === name))
    .filter((filter): filter is SearchParamFilter => filter !== undefined);
  const availableFilters = filters.filter(
    (filter) => !visibleNames.includes(filter.name) && !filter.disabled,
  );
  const active = filters.some((filter) => filter.active);

  return (
    <Paper className="filter-panel" p="md" withBorder>
      <Form method="get" onChange={handleChange} onSubmit={handleSubmit} replace>
        <Stack gap="md">
          <Group justify="space-between">
            <Group gap="xs">
              <IconFilter aria-hidden size={17} stroke={1.8} />
              <Text fw={650} size="sm">
                Filter results
              </Text>
              <Badge color="gray" size="sm" variant="light">
                Match all
              </Badge>
            </Group>
            <Group gap="xs">
              {active ? (
                <Button
                  component={Link}
                  onClick={() => setAddedNames([])}
                  size="compact-sm"
                  to={clearTo}
                  variant="subtle"
                >
                  Clear all
                </Button>
              ) : null}
              <Menu position="bottom-end" shadow="md" width={220}>
                <Menu.Target>
                  <Button
                    disabled={availableFilters.length === 0}
                    leftSection={<IconPlus aria-hidden size={15} />}
                    size="compact-sm"
                    type="button"
                    variant="light"
                  >
                    Add filter
                  </Button>
                </Menu.Target>
                <Menu.Dropdown>
                  <Menu.Label>Available filters</Menu.Label>
                  {availableFilters.map((filter) => (
                    <Menu.Item
                      key={filter.name}
                      onClick={() => setAddedNames((current) => [...current, filter.name])}
                    >
                      {filter.label}
                    </Menu.Item>
                  ))}
                </Menu.Dropdown>
              </Menu>
            </Group>
          </Group>

          {visibleFilters.length === 0 ? (
            <Text c="dimmed" size="sm">
              Showing all results. Add a filter to narrow the list.
            </Text>
          ) : (
            <Stack gap="xs">
              {visibleFilters.map((filter, index) => (
                <Group className="filter-clause" gap="xs" key={filter.name} wrap="nowrap">
                  <Text c="dimmed" className="filter-clause-prefix" size="sm">
                    {index === 0 ? "Where" : "and"}
                  </Text>
                  <Badge radius="sm" size="lg" variant="light">
                    {filter.label}
                  </Badge>
                  <Text c="dimmed" size="sm">
                    is
                  </Text>
                  <Box className="filter-clause-control">{renderFilterControl(filter)}</Box>
                  <ActionIcon
                    aria-label={`Remove ${filter.label} filter`}
                    color="gray"
                    onClick={() => removeFilter(filter)}
                    type="button"
                    variant="subtle"
                  >
                    <IconX aria-hidden size={16} />
                  </ActionIcon>
                </Group>
              ))}
            </Stack>
          )}
        </Stack>
      </Form>
    </Paper>
  );
}

function renderFilterControl(filter: SearchParamFilter) {
  if (filter.kind === "select") {
    return (
      <NativeSelect
        aria-label={`${filter.label} filter`}
        data={filter.data}
        defaultValue={filter.value ?? ""}
        key={`${filter.name}:${filter.value ?? ""}`}
        name={filter.name}
        onChange={(event) => {
          if (event.currentTarget.value !== "") return;

          for (const name of filter.clearWhenEmpty ?? []) {
            const input = event.currentTarget.form?.elements.namedItem(name);
            if (input instanceof HTMLInputElement || input instanceof HTMLSelectElement) {
              input.value = "";
            }
          }
        }}
        size="sm"
      />
    );
  }

  return (
    <TextInput
      aria-label={`${filter.label} filter`}
      defaultValue={filter.value}
      key={`${filter.name}:${filter.value ?? ""}`}
      maxLength={filter.maxLength}
      name={filter.name}
      placeholder={filter.placeholder}
      size="sm"
      type="search"
    />
  );
}

export function ListPagination({
  itemCount,
  noun,
  page,
  pageSize,
  total,
}: Readonly<{
  itemCount: number;
  noun: string;
  page: number;
  pageSize: number;
  total: number;
}>) {
  const [searchParams, setSearchParams] = useSearchParams();
  const pageCount = Math.ceil(total / pageSize);
  const firstItem = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const lastItem = firstItem + itemCount - 1;

  function changePage(nextPage: number) {
    const nextSearchParams = new URLSearchParams(searchParams);
    if (nextPage === 1) {
      nextSearchParams.delete("page");
    } else {
      nextSearchParams.set("page", String(nextPage));
    }
    setSearchParams(nextSearchParams);
  }

  return (
    <Group justify="space-between">
      <Text c="dimmed" size="sm">
        Showing {firstItem}–{lastItem} of {total} {noun}
      </Text>
      {pageCount > 1 ? (
        <Pagination onChange={changePage} total={pageCount} value={page} withEdges />
      ) : null}
    </Group>
  );
}
