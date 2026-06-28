import { Outlet, Link, useLocation } from 'react-router-dom'
import { Layout, Menu } from 'antd'
import { DesktopOutlined } from '@ant-design/icons'

const { Header, Content, Sider } = Layout

export default function AppLayout() {
  const location = useLocation()

  const selectedKey =
    location.pathname === '/' || location.pathname.startsWith('/terminal/')
      ? '/'
      : location.pathname

  const menuItems = [
    {
      key: '/',
      icon: <DesktopOutlined />,
      label: <Link to="/">Terminals</Link>,
    },
  ]

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider collapsible>
        <div
          style={{
            color: '#fff',
            textAlign: 'center',
            padding: '16px',
            fontWeight: 'bold',
            fontSize: '18px',
          }}
        >
          TeamX
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            background: '#fff',
            padding: '0 24px',
            borderBottom: '1px solid #f0f0f0',
          }}
        >
          <h2 style={{ margin: 0, lineHeight: '64px' }}>TeamX Admin</h2>
        </Header>
        <Content style={{ margin: '24px' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
