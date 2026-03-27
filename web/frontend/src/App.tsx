import { Routes, Route } from 'react-router-dom'
import Layout from './components/Layout/Layout'
import Overview from './pages/Overview'
import Explorer from './pages/Explorer'
import Entity from './pages/Entity'
import FilePage from './pages/FilePage'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Overview />} />
        <Route path="/explorer" element={<Explorer />} />
        <Route path="/entity/:id" element={<Entity />} />
        <Route path="/file/*" element={<FilePage />} />
      </Routes>
    </Layout>
  )
}
