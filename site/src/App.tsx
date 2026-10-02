import { Routes, Route } from 'react-router-dom';
import Layout from '@/components/Layout';
import Landing from '@/pages/Landing';
import GettingStarted from '@/pages/docs/GettingStarted';
import Usage from '@/pages/docs/Usage';
import Configuration from '@/pages/docs/Configuration';
import Providers from '@/pages/docs/Providers';
import Security from '@/pages/docs/Security';
import Headless from '@/pages/docs/Headless';

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Landing />} />
        <Route path="/docs" element={<GettingStarted />} />
        <Route path="/docs/usage" element={<Usage />} />
        <Route path="/docs/configuration" element={<Configuration />} />
        <Route path="/docs/providers" element={<Providers />} />
        <Route path="/docs/security" element={<Security />} />
        <Route path="/docs/headless" element={<Headless />} />
      </Routes>
    </Layout>
  );
}
