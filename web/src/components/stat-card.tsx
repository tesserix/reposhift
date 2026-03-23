interface StatCardProps {
  label: string;
  value: number | string;
  trend?: {
    value: string;
    positive?: boolean;
  };
  icon?: React.ReactNode;
  accentColor?: string;
}

function StatCard({ label, value, trend, icon, accentColor }: StatCardProps) {
  return (
    <div className="rounded-xl border border-card-border bg-card p-5">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </p>
        {icon && (
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-secondary text-muted-foreground">
            {icon}
          </div>
        )}
      </div>
      <p className={`mt-3 text-3xl font-semibold ${accentColor || "text-foreground"}`}>
        {value}
      </p>
      {trend && (
        <p className={`mt-1 text-xs ${trend.positive ? "text-emerald-400" : "text-red-400"}`}>
          {trend.value}
        </p>
      )}
    </div>
  );
}

export { StatCard };
