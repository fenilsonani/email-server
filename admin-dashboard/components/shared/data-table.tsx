"use client";

import { useState } from "react";
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
  type ColumnFiltersState,
} from "@tanstack/react-table";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
  Search,
} from "lucide-react";

interface DataTableProps<TData, TValue> {
  columns: ColumnDef<TData, TValue>[];
  data: TData[];
  loading?: boolean;
  searchKey?: string;
  searchPlaceholder?: string;
  pageSize?: number;
  emptyMessage?: string;
  toolbar?: React.ReactNode;
  /** Server-side pagination */
  serverPagination?: {
    page: number;
    totalPages: number;
    totalCount: number;
    onPageChange: (page: number) => void;
  };
}

export function DataTable<TData, TValue>({
  columns,
  data,
  loading = false,
  searchKey,
  searchPlaceholder = "Search...",
  pageSize = 20,
  emptyMessage = "No results.",
  toolbar,
  serverPagination,
}: DataTableProps<TData, TValue>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [globalFilter, setGlobalFilter] = useState("");

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting,
      columnFilters,
      globalFilter: searchKey ? undefined : globalFilter,
    },
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    ...(!serverPagination && { getPaginationRowModel: getPaginationRowModel() }),
    initialState: {
      pagination: { pageSize },
    },
  });

  const currentPage = serverPagination?.page ?? table.getState().pagination.pageIndex + 1;
  const totalPages = serverPagination?.totalPages ?? table.getPageCount();
  const totalRows = serverPagination?.totalCount ?? table.getFilteredRowModel().rows.length;

  if (loading) {
    return (
      <div className="space-y-3">
        {toolbar && <div className="flex items-center gap-2">{toolbar}</div>}
        <div className="rounded-lg border border-border">
          <div className="border-b border-border px-4 py-2.5">
            <Skeleton className="h-4 w-48" />
          </div>
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="border-b border-border last:border-0 px-4 py-3">
              <Skeleton className="h-4 w-full" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Toolbar row */}
      {(searchKey || toolbar) && (
        <div className="flex items-center gap-2">
          {searchKey && (
            <div className="relative max-w-[240px]">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground/50" strokeWidth={1.5} />
              <Input
                placeholder={searchPlaceholder}
                value={(table.getColumn(searchKey)?.getFilterValue() as string) ?? ""}
                onChange={(e) => table.getColumn(searchKey)?.setFilterValue(e.target.value)}
                className="h-8 pl-8 text-[12px] bg-background/50 border-border placeholder:text-muted-foreground/40"
              />
            </div>
          )}
          {toolbar}
        </div>
      )}

      {/* Table */}
      <div className="rounded-lg border border-border overflow-hidden">
        <table className="w-full text-[13px]">
          <thead>
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id} className="border-b border-border bg-muted/30">
                {headerGroup.headers.map((header) => (
                  <th
                    key={header.id}
                    className={cn(
                      "h-9 px-3 text-left align-middle text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60",
                      header.column.getCanSort() && "cursor-pointer select-none hover:text-muted-foreground"
                    )}
                    onClick={header.column.getToggleSortingHandler()}
                  >
                    <div className="flex items-center gap-1">
                      {header.isPlaceholder
                        ? null
                        : flexRender(header.column.columnDef.header, header.getContext())}
                      {header.column.getCanSort() && (
                        <span className="ml-0.5">
                          {header.column.getIsSorted() === "asc" ? (
                            <ArrowUp className="h-3 w-3" />
                          ) : header.column.getIsSorted() === "desc" ? (
                            <ArrowDown className="h-3 w-3" />
                          ) : (
                            <ArrowUpDown className="h-3 w-3 opacity-30" />
                          )}
                        </span>
                      )}
                    </div>
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.length === 0 ? (
              <tr>
                <td
                  colSpan={columns.length}
                  className="h-24 text-center text-[13px] text-muted-foreground/50"
                >
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              table.getRowModel().rows.map((row) => (
                <tr
                  key={row.id}
                  className="border-b border-border last:border-0 transition-colors hover:bg-muted/20"
                >
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="px-3 py-2.5 align-middle">
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {(totalPages > 1 || totalRows > 0) && (
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-muted-foreground/50 tabular-nums">
            {totalRows} result{totalRows !== 1 ? "s" : ""}
          </span>
          {totalPages > 1 && (
            <div className="flex items-center gap-1">
              {!serverPagination && (
                <button
                  onClick={() => table.setPageIndex(0)}
                  disabled={!table.getCanPreviousPage()}
                  className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                  <ChevronsLeft className="h-3.5 w-3.5" />
                </button>
              )}
              <button
                onClick={() => {
                  if (serverPagination) {
                    serverPagination.onPageChange(Math.max(1, currentPage - 1));
                  } else {
                    table.previousPage();
                  }
                }}
                disabled={serverPagination ? currentPage <= 1 : !table.getCanPreviousPage()}
                className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
              >
                <ChevronLeft className="h-3.5 w-3.5" />
              </button>
              <span className="px-2 text-[11px] text-muted-foreground/60 tabular-nums">
                {currentPage} / {totalPages}
              </span>
              <button
                onClick={() => {
                  if (serverPagination) {
                    serverPagination.onPageChange(Math.min(totalPages, currentPage + 1));
                  } else {
                    table.nextPage();
                  }
                }}
                disabled={serverPagination ? currentPage >= totalPages : !table.getCanNextPage()}
                className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
              >
                <ChevronRight className="h-3.5 w-3.5" />
              </button>
              {!serverPagination && (
                <button
                  onClick={() => table.setPageIndex(table.getPageCount() - 1)}
                  disabled={!table.getCanNextPage()}
                  className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                  <ChevronsRight className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Sort header helper — wrap column header text to make it sortable */
export function SortableHeader({ label }: { label: string }) {
  return <span>{label}</span>;
}
