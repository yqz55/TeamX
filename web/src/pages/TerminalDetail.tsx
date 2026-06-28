import { useParams, useNavigate } from 'react-router-dom'
import {
  Typography,
  Card,
  Tabs,
  Descriptions,
  Button,
  Space,
} from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'

export default function TerminalDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/')}>
          Back
        </Button>
      </Space>

      <Typography.Title level={3}>Terminal: {id}</Typography.Title>
      <Typography.Paragraph type="secondary">
        Full detail page will be implemented in Phase 4b.3.
      </Typography.Paragraph>

      <Card>
        <Descriptions title="Basic Info" column={2} bordered>
          <Descriptions.Item label="Session ID">{id}</Descriptions.Item>
          <Descriptions.Item label="Status">-</Descriptions.Item>
          <Descriptions.Item label="Hostname">-</Descriptions.Item>
          <Descriptions.Item label="OS">-</Descriptions.Item>
        </Descriptions>
      </Card>

      <Tabs
        style={{ marginTop: 16 }}
        items={[
          {
            key: 'overview',
            label: 'Overview',
            children: (
              <Card>
                <Typography.Paragraph type="secondary">
                  Connection status and system overview will appear here.
                </Typography.Paragraph>
              </Card>
            ),
          },
          {
            key: 'hardware',
            label: 'Hardware',
            children: (
              <Card>
                <Typography.Paragraph type="secondary">
                  CPU, Memory, Disks, Network, BIOS, Motherboard info will
                  appear here.
                </Typography.Paragraph>
              </Card>
            ),
          },
          {
            key: 'software',
            label: 'Software',
            children: (
              <Card>
                <Typography.Paragraph type="secondary">
                  Installed software list will appear here (Phase 6).
                </Typography.Paragraph>
              </Card>
            ),
          },
          {
            key: 'processes',
            label: 'Processes',
            children: (
              <Card>
                <Typography.Paragraph type="secondary">
                  Running processes will appear here (Phase 6).
                </Typography.Paragraph>
              </Card>
            ),
          },
        ]}
      />
    </div>
  )
}
