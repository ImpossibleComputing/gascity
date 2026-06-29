import { lazy } from 'react';
import type { FrontendViewDescriptor } from '../types';

// Formulas is a core view: always mounted (it just lists what the city's
// supervisor serves), nav-ordered 35 so it sits between Beads (30) and Runs
// (40) — formula definitions read left-to-right into the runs they produce.
// The lazy import keeps the catalog's chunk out of the default first-paint
// bundle; the route only loads when the operator opens /formulas.

export const formulasView: FrontendViewDescriptor = {
  id: 'formulas',
  kind: 'core',
  path: '/formulas',
  nav: { label: 'Formulas', order: 35 },
  element: lazy(() => import('../../routes/Formulas').then((m) => ({ default: m.FormulasPage }))),
};
