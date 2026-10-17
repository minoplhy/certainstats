// Parse User-Agent string to readable OS and Browser
export function parseUserAgent(ua) {
  if (!ua) return { os: 'Unknown OS', browser: 'Unknown Browser', device: 'Desktop' };
  let os = 'Unknown OS';
  let browser = 'Unknown Browser';
  let isMobile = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(ua);

  if (/Windows NT 10/i.test(ua)) os = 'Windows 10/11';
  else if (/Windows NT/i.test(ua)) os = 'Windows';
  else if (/Macintosh|Mac OS X/i.test(ua)) os = 'macOS';
  else if (/Android/i.test(ua)) os = 'Android';
  else if (/iPhone|iPad|iPod/i.test(ua)) os = 'iOS';
  else if (/Linux/i.test(ua)) os = 'Linux';

  if (/Edg\//i.test(ua)) browser = 'Edge';
  else if (/Chrome\//i.test(ua)) browser = 'Chrome';
  else if (/Safari\//i.test(ua) && !/Chrome/i.test(ua)) browser = 'Safari';
  else if (/Firefox\//i.test(ua)) browser = 'Firefox';
  else if (/MSIE|Trident/i.test(ua)) browser = 'Internet Explorer';

  return {
    os: os,
    browser: browser,
    label: `${browser} on ${os}`,
    device: isMobile ? 'Mobile' : 'Desktop'
  };
}
