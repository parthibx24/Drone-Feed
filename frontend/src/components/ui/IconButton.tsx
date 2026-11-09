import type { AnchorHTMLAttributes, ButtonHTMLAttributes } from "react";
import styles from "./IconButton.module.css";

type Variant = "control" | "tile";

interface IconProps {
  icon: string;
  label: string;
  variant?: Variant;
  active?: boolean;
}

function classes(variant: Variant, active: boolean, className: string): string {
  return `${styles.button} ${styles[variant]} ${active ? styles.active : ""} ${className}`;
}

export function IconButton({
  icon,
  label,
  variant = "control",
  active = false,
  className = "",
  ...props
}: IconProps & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={classes(variant, active, className)}
      aria-label={label}
      title={label}
      {...props}
    >
      <img className={styles.icon} src={icon} alt="" />
    </button>
  );
}

export function IconLink({
  icon,
  label,
  variant = "control",
  active = false,
  className = "",
  ...props
}: IconProps & AnchorHTMLAttributes<HTMLAnchorElement>) {
  return (
    <a className={classes(variant, active, className)} aria-label={label} title={label} {...props}>
      <img className={styles.icon} src={icon} alt="" />
    </a>
  );
}
