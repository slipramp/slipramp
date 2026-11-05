import { stringify } from "yaml";

type StringKeyOf<T> = Extract<keyof T, string>;

export type FieldOrder<T extends object> = {
  [Key in StringKeyOf<T>]-?: number;
};

export type FieldProjection<T extends object> = {
  [Key in StringKeyOf<T>]-?: number | null;
};

export function reorderFields<T extends object>(value: T, order: FieldOrder<T>): T {
  return Object.fromEntries(
    Object.entries(value).toSorted(([left], [right]) => {
      const leftRank = order[left as StringKeyOf<T>] ?? Number.MAX_SAFE_INTEGER;
      const rightRank = order[right as StringKeyOf<T>] ?? Number.MAX_SAFE_INTEGER;
      return leftRank - rightRank || left.localeCompare(right);
    }),
  ) as T;
}

export function projectFields<T extends object>(
  value: T,
  order: FieldProjection<T>,
): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(value)
      .filter(([key]) => order[key as StringKeyOf<T>] !== null)
      .toSorted(([left], [right]) => {
        const leftRank = order[left as StringKeyOf<T>] ?? Number.MAX_SAFE_INTEGER;
        const rightRank = order[right as StringKeyOf<T>] ?? Number.MAX_SAFE_INTEGER;
        return leftRank - rightRank || left.localeCompare(right);
      }),
  );
}

export function sortRecord<T>(value: Record<string, T>): Record<string, T> {
  return Object.fromEntries(
    Object.entries(value).toSorted(([left], [right]) => left.localeCompare(right)),
  );
}

export function mapRecord<T, U>(
  value: Record<string, T>,
  transform: (item: T) => U,
): Record<string, U> {
  return Object.fromEntries(
    Object.entries(value)
      .toSorted(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => [key, transform(item)]),
  );
}

export function renderYamlDefinition(value: unknown) {
  return stringify(value, { lineWidth: 0 });
}
