"use client";

import React, { useRef } from "react";
import { NavLink, useLocation } from "react-router-dom";
import {
  motion,
  useMotionValue,
  useSpring,
  useTransform,
  MotionValue,
} from "framer-motion";
import { cn } from "@/lib/utils";

interface DockIconProps {
  mouseX?: MotionValue<number>;
  to: string;
  label: string;
  children: React.ReactNode;
  onClick?: () => void;
}

const DockIcon: React.FC<DockIconProps> = ({
  mouseX,
  to,

  children,
  onClick,
}) => {
  const ref = useRef<HTMLDivElement>(null);
  const location = useLocation();
  const defaultMouseX = useMotionValue(Infinity);

  const iconSize = 44;
  const iconMagnification = 64;
  const iconDistance = 100;

  const isActive = location.pathname === to ||
    (to !== '/' && location.pathname.startsWith(to));

  const distance = useTransform(mouseX ?? defaultMouseX, (val) => {
    const bounds = ref.current?.getBoundingClientRect() ?? { x: 0, width: 0 };
    return val - bounds.x - bounds.width / 2;
  });

  const widthSync = useTransform(
    distance,
    [-iconDistance, 0, iconDistance],
    [iconSize, iconMagnification, iconSize]
  );

  const width = useSpring(widthSync, {
    mass: 0.1,
    stiffness: 150,
    damping: 12,
  });

  return (
    <motion.div
      ref={ref}
      style={{ width }}
      className="flex aspect-square items-center justify-center"
    >
      <NavLink
        to={to}
        onClick={onClick}
        className={cn(
          "flex h-full w-full items-center justify-center rounded-xl transition-colors",
          isActive
            ? "bg-primary/30 text-white"
            : "bg-white/10 text-muted-foreground hover:bg-white/20 hover:text-white"
        )}
      >
        <div className="flex flex-col items-center justify-center gap-0.5">
          <div className="p-1.5">{children}</div>
        </div>
      </NavLink>
    </motion.div>
  );
};

interface DockProps {
  children: React.ReactNode;
  className?: string;
}

const Dock: React.FC<DockProps> = ({ children, className }) => {
  const mouseX = useMotionValue(Infinity);

  return (
    <motion.div
      onMouseMove={(e) => mouseX.set(e.pageX)}
      onMouseLeave={() => mouseX.set(Infinity)}
      className={cn(
        "flex h-[64px] items-center gap-1.5 rounded-2xl px-2",
        "bg-[rgba(12,12,18,0.95)] border border-white/10 backdrop-blur-xl",
        "shadow-[0_8px_32px_rgba(0,0,0,0.4)]",
        className
      )}
    >
      {React.Children.map(children, (child) => {
        if (React.isValidElement(child) && child.type === DockIcon) {
          return React.cloneElement(
            child as React.ReactElement<DockIconProps>,
            {
              ...(child.props as DockIconProps),
              mouseX: mouseX,
            }
          );
        }
        return child;
      })}
    </motion.div>
  );
};

// Separator for dock
const DockSeparator: React.FC = () => (
  <div className="h-8 w-px bg-white/10 mx-1" />
);

export { Dock, DockIcon, DockSeparator };
