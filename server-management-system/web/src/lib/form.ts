import type { UseFormSetError, FieldValues, Path } from "react-hook-form";
import { ApiError } from "@/lib/api/client";
import { toast } from "sonner";

/**
 * Map an ApiError's field-level errors onto a react-hook-form, falling back to
 * a toast for the top-level message. Returns true if it was an ApiError.
 */
export function handleFormError<T extends FieldValues>(
  err: unknown,
  setError: UseFormSetError<T>,
): boolean {
  if (err instanceof ApiError) {
    if (err.fieldErrors.length) {
      for (const fe of err.fieldErrors) {
        setError(fe.field as Path<T>, { type: "server", message: fe.message });
      }
    }
    toast.error(err.message);
    return true;
  }
  toast.error("Có lỗi xảy ra, vui lòng thử lại.");
  return false;
}

export function errorMessage(err: unknown, fallback = "Có lỗi xảy ra"): string {
  return err instanceof ApiError ? err.message : fallback;
}
