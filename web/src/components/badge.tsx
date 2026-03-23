import { type HTMLAttributes } from "react";

type BadgeVariant = "default" | "success" | "warning" | "error" | "info";

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant;
}

const variantStyles: Record<BadgeVariant, string> = {
  default: "border-border bg-secondary text-muted-foreground",
  success: "border-emerald-800/60 bg-emerald-950/50 text-emerald-400",
  warning: "border-yellow-800/60 bg-yellow-950/50 text-yellow-400",
  error: "border-red-800/60 bg-red-950/50 text-red-400",
  info: "border-blue-800/60 bg-blue-950/50 text-blue-400",
};

function Badge({ className = "", variant = "default", ...props }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium capitalize ${variantStyles[variant]} ${className}`}
      {...props}
    />
  );
}

function StatusBadge({ status }: { status: string }) {
  const variantMap: Record<string, BadgeVariant> = {
    completed: "success",
    running: "info",
    in_progress: "info",
    failed: "error",
    paused: "warning",
    cancelled: "default",
    pending: "default",
  };

  return (
    <Badge variant={variantMap[status] || "default"}>
      {status.replace(/_/g, " ")}
    </Badge>
  );
}

export { Badge, StatusBadge, type BadgeVariant };
