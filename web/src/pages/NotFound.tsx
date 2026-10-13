import { Compass } from "lucide-react";
import { Link } from "react-router";
import { Button, EmptyState } from "../components/ui";
import { useI18n } from "../lib/i18n";

export function NotFoundPage() {
  const { t } = useI18n();
  return (
    <EmptyState
      icon={<Compass className="size-5" />}
      title={t("notFound.title")}
      action={
        <Link to="/">
          <Button variant="secondary">{t("notFound.back")}</Button>
        </Link>
      }
    />
  );
}
