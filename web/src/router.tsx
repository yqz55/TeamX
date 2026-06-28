import { createBrowserRouter } from 'react-router-dom'
import AppLayout from './components/AppLayout'
import TerminalList from './pages/TerminalList'
import TerminalDetail from './pages/TerminalDetail'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppLayout />,
    children: [
      { index: true, element: <TerminalList /> },
      { path: 'terminal/:id', element: <TerminalDetail /> },
    ],
  },
])
