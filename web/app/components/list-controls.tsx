import { Button, Group, Loader, Pagination, Paper, Stack, Text } from "@mantine/core";
import { useDebouncedCallback } from "@mantine/hooks";
import { type FormEvent, type ReactNode } from "react";
import { Form, Link, useNavigation, useSearchParams, useSubmit } from "react-router";

const filterDebounceMs = 250;

export function SearchParamFilters({
  active,
  children,
  clearTo,
}: Readonly<{
  active: boolean;
  children: ReactNode;
  clearTo: string;
}>) {
  const navigation = useNavigation();
  const submit = useSubmit();
  const submitTextFilters = useDebouncedCallback(submitFilters, filterDebounceMs);

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
    const searchParams = new URLSearchParams();
    for (const [name, value] of new FormData(form)) {
      if (typeof value === "string" && value.trim() !== "") {
        searchParams.append(name, value.trim());
      }
    }
    submit(searchParams, { method: "get", replace: true });
  }

  return (
    <Paper className="filter-panel" p="md" withBorder>
      <Form method="get" onChange={handleChange} onSubmit={handleSubmit} replace>
        <Stack gap="sm">
          <Group align="flex-end" gap="md" wrap="wrap">
            {children}
            {active ? (
              <Button component={Link} to={clearTo} variant="subtle">
                Clear
              </Button>
            ) : null}
          </Group>
          {navigation.state === "loading" ? (
            <Group gap={6} justify="flex-end">
              <Loader size={12} />
              <Text c="dimmed" size="xs">
                Updating
              </Text>
            </Group>
          ) : null}
        </Stack>
      </Form>
    </Paper>
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
