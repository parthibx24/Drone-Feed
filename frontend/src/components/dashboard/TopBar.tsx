import { useEffect, useRef, useState } from "react";
import { NavLink, useNavigate } from "react-router";
import logo from "../../assets/brand/logo.png";
import arrowDown from "../../assets/icons/arrow-down.svg";
import policeBadge from "../../assets/icons/police-badge.svg";
import { useAuth } from "../../context/AuthContext";
import type { LinkState } from "../../types/drone";
import styles from "./TopBar.module.css";

export interface LinkStatus {
  label: string;
  state: LinkState;
}

export interface NavItem {
  to: string;
  label: string;
  end?: boolean;
}

interface TopBarProps {
  /** Drone name, or a page title such as "Fleet". */
  title: string;
  links?: LinkStatus[];
  nav?: NavItem[];
}

export function TopBar({ title, links = [], nav = [] }: TopBarProps) {
  const { me, logout } = useAuth();
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menuOpen) return;
    const close = (event: MouseEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setMenuOpen(false);
    };
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [menuOpen]);

  return (
    <header className={styles.bar}>
      <div className={styles.left}>
        <button type="button" className={styles.logoFrame} onClick={() => navigate("/")} title="All drones">
          <img className={styles.logo} src={logo} alt="StarWatch" />
        </button>
        {nav.length > 0 && (
          <nav className={styles.nav}>
            {nav.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) => `${styles.tab} ${isActive ? styles.tabActive : ""}`}
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
        )}
      </div>

      <div className={styles.links}>
        {links.map((link) => (
          <div key={link.label} className={styles.link}>
            <span className={`${styles.dot} ${styles[link.state]}`} />
            {link.label}
          </div>
        ))}
      </div>

      <div className={styles.accountWrap} ref={menuRef}>
        <button type="button" className={styles.account} onClick={() => setMenuOpen((open) => !open)}>
          <span className={styles.avatar}>
            <img className={styles.badge} src={policeBadge} alt="" />
          </span>
          <span className={styles.droneName}>{title}</span>
          <img className={styles.arrow} src={arrowDown} alt="" />
        </button>
        {menuOpen && (
          <div className={styles.menu} role="menu">
            {me && (
              <div className={styles.menuUser}>
                <span>{me.username}</span>
                <span className={styles.role}>{me.role}</span>
              </div>
            )}
            <button type="button" role="menuitem" className={styles.menuItem} onClick={() => navigate("/")}>
              All drones
            </button>
            {me?.auth_enabled && (
              <button
                type="button"
                role="menuitem"
                className={styles.menuItem}
                onClick={() => void logout().then(() => navigate("/login"))}
              >
                Log out
              </button>
            )}
          </div>
        )}
      </div>
    </header>
  );
}
