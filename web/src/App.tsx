import { HashRouter, Routes, Route, useLocation } from 'react-router-dom'
import { useEffect } from 'react'

function ScrollToTop() {
  const { pathname } = useLocation()
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])
  return null
}
import { Home } from './pages/Home'
import { Login } from './pages/Login'
import { Pricing } from './pages/Pricing'
import { Docs } from './pages/Docs'
import { DashboardLayout } from './pages/dashboard/Layout'
import { Profile } from './pages/dashboard/Profile'
import { Wallet } from './pages/dashboard/Wallet'
import { ApiKeys } from './pages/dashboard/ApiKeys'
import { TaskLogs } from './pages/dashboard/TaskLogs'
import { CreditLogs } from './pages/dashboard/CreditLogs'
import { AdminUsers } from './pages/admin/Users'
import { AdminVendors } from './pages/admin/Vendors'
import { AdminEndpoints } from './pages/admin/Endpoints'
import { AdminModels } from './pages/admin/Models'
import { AdminVendorModels } from './pages/admin/VendorModels'
import { AdminCreditLogs } from './pages/admin/CreditLogs'
import { AdminTaskLogs } from './pages/admin/TaskLogs'

function App() {
  return (
    <HashRouter>
      <ScrollToTop />
      <Routes>
        <Route path='/' element={<Home />} />
        <Route path='/login' element={<Login />} />
        <Route path='/pricing' element={<Pricing />} />
        <Route path='/docs' element={<Docs />} />
        <Route path='/docs/:slug' element={<Docs />} />

        {/* 控制台 */}
        <Route path='/dashboard' element={<DashboardLayout />}>
          <Route path='profile' element={<Profile />} />
          <Route path='wallet' element={<Wallet />} />
          <Route path='api-keys' element={<ApiKeys />} />
          <Route path='task-logs' element={<TaskLogs />} />
          <Route path='credit-logs' element={<CreditLogs />} />
          <Route path='admin/users' element={<AdminUsers />} />
          <Route path='admin/endpoints' element={<AdminEndpoints />} />
          <Route path='admin/vendors' element={<AdminVendors />} />
          <Route path='admin/models' element={<AdminModels />} />
          <Route path='admin/vendor-models' element={<AdminVendorModels />} />
          <Route path='admin/credit-logs' element={<AdminCreditLogs />} />
          <Route path='admin/task-logs' element={<AdminTaskLogs />} />
        </Route>
      </Routes>
    </HashRouter>
  )
}

export default App
