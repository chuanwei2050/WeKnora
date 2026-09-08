UPDATE knowledges
SET preview_status = 'none',
    preview_file_path = '',
    preview_error = '',
    full_preview_status = 'none',
    full_preview_file_path = '',
    full_preview_error = ''
WHERE preview_status <> 'none'
   OR preview_file_path <> ''
   OR full_preview_status <> 'none'
   OR full_preview_file_path <> '';
