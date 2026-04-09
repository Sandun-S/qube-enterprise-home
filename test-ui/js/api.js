/**
 * Qube Enterprise API Wrapper
 * Centralized fetch handler with JWT and error management.
 */

const API = {
  userRole: localStorage.getItem('qube_user_role') || '',

  setUserRole(role) {
    this.userRole = role;
    localStorage.setItem('qube_user_role', role);
  },

  baseUrl: (() => {
    const stored = localStorage.getItem('qube_api_url');
    const fallback = `${window.location.protocol}//${window.location.hostname}:8080`;
    if (stored) {
      try {
        const url = new URL(stored);
        // If accessing via actual IP/hostname but config is stuck on localhost, use fallback
        if (window.location.hostname !== 'localhost' && url.hostname === 'localhost') return fallback;
        return stored;
      } catch (e) { return fallback; }
    }
    return fallback;
  })(),
  token: localStorage.getItem('qube_token') || '',

  setBaseUrl(url) {
    this.baseUrl = url.replace(/\/$/, '');
    localStorage.setItem('qube_api_url', this.baseUrl);
  },

  setToken(token) {
    this.token = token;
    localStorage.setItem('qube_token', token);
  },

  logout() {
    this.token = '';
    this.userRole = '';
    localStorage.removeItem('qube_token');
    localStorage.removeItem('qube_user_role');
    window.location.hash = '#login';
  },

  async request(method, path, body = null) {
    const url = `${this.baseUrl}${path}`;
    const options = {
      method,
      headers: {
        'Content-Type': 'application/json',
      }
    };

    if (this.token) {
      options.headers['Authorization'] = `Bearer ${this.token}`;
    }

    if (body) {
      options.body = JSON.stringify(body);
    }

    try {
      // Diagnostic log
      console.log(`[API] ${method} ${url.toString()}`);

      const response = await fetch(url.toString(), options);
      
      // Handle 401 Unauthorized
      if (response.status === 401 && !path.includes('/auth/login')) {
        this.logout();
        throw new Error('Session expired. Please login again.');
      }

      const data = await response.json().catch(() => ({}));

      if (!response.ok) {
        throw new Error(data.error || `API Error: ${response.status}`);
      }

      return data;
    } catch (err) {
      console.error(`API Request Failed: ${method} ${path}`, err);
      throw err;
    }
  },

  get(path) { return this.request('GET', path); },
  post(path, body) { return this.request('POST', path, body); },
  put(path, body) { return this.request('PUT', path, body); },
  delete(path) { return this.request('DELETE', path); },

  // Auth
  login(email, password) {
    return this.request('POST', '/api/v1/auth/login', { email, password });
  },
  register(org_name, email, password) {
    return this.request('POST', '/api/v1/auth/register', { org_name, email, password });
  },
  getMe() {
    return this.request('GET', '/api/v1/users/me');
  },

  // Qubes
  getQubes() {
    return this.request('GET', '/api/v1/qubes');
  },
  getQube(id) {
    return this.request('GET', `/api/v1/qubes/${id}`);
  },
  claimQube(register_key) {
    return this.request('POST', '/api/v1/qubes/claim', { register_key });
  },
  unclaimQube(id) {
    return this.request('POST', `/api/v1/qubes/${id}/unclaim`);
  },
  getAllQubesAdmin() {
    return this.request('GET', '/api/v1/admin/qubes');
  },
  getQubeReaders(id) {
    return this.request('GET', `/api/v1/qubes/${id}/readers`);
  },

  // Protocols & Templates
  getProtocols() {
    return this.request('GET', '/api/v1/protocols');
  },

  getReaderTemplates(protocol) {
    let path = '/api/v1/reader-templates';
    if (protocol) path += `?protocol=${protocol}`;
    return this.request('GET', path);
  },

  getDeviceTemplates(protocol) {
    let path = '/api/v1/device-templates';
    if (protocol) path += `?protocol=${protocol}`;
    return this.request('GET', path);
  },
  getDeviceTemplate(id) {
    return this.request('GET', `/api/v1/device-templates/${id}`);
  },

  // Readers & Sensors
  createReader(qubeId, data) {
    return this.request('POST', `/api/v1/qubes/${qubeId}/readers`, data);
  },
  getReader(id) {
    return this.request('GET', `/api/v1/readers/${id}`);
  },
  updateReader(id, data) {
    return this.request('PUT', `/api/v1/readers/${id}`, data);
  },
  deleteReader(id) {
    return this.request('DELETE', `/api/v1/readers/${id}`);
  },
  getReaderSensors(id) {
    return this.request('GET', `/api/v1/readers/${id}/sensors`);
  },
  createSensor(readerId, data) {
    return this.request('POST', `/api/v1/readers/${readerId}/sensors`, data);
  },
  updateSensor(sensorId, data) {
    return this.request('PUT', `/api/v1/sensors/${sensorId}`, data);
  },
  deleteSensor(sensorId) {
    return this.request('DELETE', `/api/v1/sensors/${sensorId}`);
  },

  // Telemetry
  getTelemetry(params) {
    const query = new URLSearchParams(params).toString();
    return this.request('GET', `/api/v1/telemetry?${query}`);
  },

  // Users
  getUsers() {
    return this.request('GET', '/api/v1/users');
  },

  // Protocol management (superadmin)
  getAllProtocolsAdmin() {
    return this.request('GET', '/api/v1/admin/protocols');
  },
  createProtocol(data) {
    return this.request('POST', '/api/v1/admin/protocols', data);
  },
  updateProtocol(id, data) {
    return this.request('PUT', `/api/v1/admin/protocols/${id}`, data);
  },
  deleteProtocol(id) {
    return this.request('DELETE', `/api/v1/admin/protocols/${id}`);
  }
};

export default API;
