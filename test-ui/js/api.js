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
  async getProtocols() {
    const protocols = await this.request('GET', '/api/v1/protocols');
    
    // Fallback for new protocols (BACnet, LoRaWAN, DNP3) if not yet in DB
    const newProtocols = [
      { id: 'bacnet', label: 'BACnet/IP', description: 'Building automation (HVAC, chiller, lighting)', reader_standard: 'multi_target', is_active: true },
      { id: 'lorawan', label: 'LoRaWAN', description: 'Long-range low-power IoT (Chirpstack, TTN)', reader_standard: 'endpoint', is_active: true },
      { id: 'dnp3', label: 'DNP3', description: 'Utility SCADA (substations, RTUs, water treatment)', reader_standard: 'endpoint', is_active: true }
    ];

    newProtocols.forEach(p => {
      if (!protocols.find(db_p => db_p.id === p.id)) protocols.push(p);
    });

    return protocols;
  },

  async getReaderTemplates(protocol) {
    let path = '/api/v1/reader-templates';
    if (protocol) path += `?protocol=${protocol}`;
    const templates = await this.request('GET', path);

    // Fallback reader templates for new protocols
    const fallbacks = {
      'bacnet': { id: 'fallback-rt-bacnet', protocol: 'bacnet', name: 'BACnet/IP Reader', image_suffix: 'bacnet-reader', connection_schema: { type: 'object', properties: { local_port: { type: 'integer', title: 'Local UDP Port', default: 47808 }, poll_interval_sec: { type: 'integer', title: 'Poll Interval', default: 30 }, broadcast_addr: { type: 'string', title: 'Broadcast Address' } } } },
      'lorawan': { id: 'fallback-rt-lorawan', protocol: 'lorawan', name: 'LoRaWAN NS Reader', image_suffix: 'lorawan-reader', connection_schema: { type: 'object', properties: { ns_host: { type: 'string', title: 'NS Host' }, ns_port: { type: 'integer', title: 'Port', default: 1700 }, app_id: { type: 'string', title: 'App ID' }, api_key: { type: 'string', title: 'API Key', format: 'password' } } } },
      'dnp3': { id: 'fallback-rt-dnp3', protocol: 'dnp3', name: 'DNP3 Reader', image_suffix: 'dnp3-reader', connection_schema: { type: 'object', properties: { host: { type: 'string', title: 'Outstation IP' }, port: { type: 'integer', title: 'Port', default: 20000 }, outstation_address: { type: 'integer', title: 'Outstation DNP3 Address', default: 10 } } } }
    };

    if (protocol && fallbacks[protocol] && !templates.find(t => t.protocol === protocol)) {
      templates.push(fallbacks[protocol]);
    }

    return templates;
  },

  async getDeviceTemplates(protocol) {
    let path = '/api/v1/device-templates';
    if (protocol) path += `?protocol=${protocol}`;
    const templates = await this.request('GET', path);

    // Fallback device templates for testing
    const fallbacks = [
      { id: 'dt-bacnet-hvac', protocol: 'bacnet', name: 'BACnet HVAC Controller', manufacturer: 'Generic', model: 'HVAC-01', description: 'Zone temp, setpoint, fan status', is_global: true, sensor_config: { objects: [{ field_key: 'zone_temp_c', object_type: 'analogInput', object_instance: 1, unit: 'C' }] }, sensor_params_schema: { type: 'object', properties: { ip_address: { type: 'string', title: 'Device IP' }, device_instance: { type: 'integer', title: 'BACnet Instance' } } } },
      { id: 'dt-lorawan-dragino', protocol: 'lorawan', name: 'Dragino LHT65', manufacturer: 'Dragino', model: 'LHT65', description: 'Temp/Humidity sensor', is_global: true, sensor_config: { readings: [{ field_key: 'temperature_c', field: 'TempC_SHT', unit: 'C' }] }, sensor_params_schema: { type: 'object', properties: { dev_eui: { type: 'string', title: 'Device EUI' } } } }
    ];

    fallbacks.forEach(t => {
      if ((!protocol || t.protocol === protocol) && !templates.find(db_t => db_t.id === t.id)) {
        templates.push(t);
      }
    });

    return templates;
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
  }
};

export default API;
