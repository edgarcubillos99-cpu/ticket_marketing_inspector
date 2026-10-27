<?php
/**
 * Plugin Name: OSNET Email Log API
 * Description: Endpoint REST de solo lectura con el recuento del Email Log de WP Mail SMTP Pro (usado por agente_marketing).
 * Version:     1.0.0
 * Author:      OSNET
 *
 * Instalación: subir este archivo a wp-content/mu-plugins/ (crear la carpeta si no existe).
 * Los mu-plugins se activan solos y no aparecen en la lista normal de plugins
 * (se ven en Plugins → "Must-Use").
 *
 * Endpoint:
 *   GET /wp-json/osnet/v1/email-log/counts?from=YYYY-MM-DD&to=YYYY-MM-DD[&initiator=Contact%20Form%207]
 *
 * Autenticación: usuario WordPress con capacidad manage_options + Application Password (Basic Auth).
 * Solo devuelve recuentos agrupados por hora/asunto/origen: NO expone destinatarios ni contenido.
 */

if ( ! defined( 'ABSPATH' ) ) {
	exit;
}

add_action( 'rest_api_init', function () {
	register_rest_route(
		'osnet/v1',
		'/email-log/counts',
		array(
			'methods'             => 'GET',
			'callback'            => 'osnet_email_log_counts',
			'permission_callback' => function () {
				return current_user_can( 'manage_options' );
			},
			'args'                => array(
				'from'      => array(
					'required'          => true,
					'validate_callback' => 'osnet_email_log_valid_date',
				),
				'to'        => array(
					'required'          => true,
					'validate_callback' => 'osnet_email_log_valid_date',
				),
				'initiator' => array(
					'required'          => false,
					'sanitize_callback' => 'sanitize_text_field',
				),
			),
		)
	);
} );

function osnet_email_log_valid_date( $value ) {
	return is_string( $value ) && preg_match( '/^\d{4}-\d{2}-\d{2}$/', $value );
}

function osnet_email_log_counts( WP_REST_Request $request ) {
	global $wpdb;

	$table = $wpdb->prefix . 'wpmailsmtp_emails_log';
	$found = $wpdb->get_var( $wpdb->prepare( 'SHOW TABLES LIKE %s', $table ) );
	if ( $found !== $table ) {
		return new WP_Error(
			'osnet_no_table',
			sprintf( 'No existe la tabla %s. ¿Está activo el Email Log de WP Mail SMTP Pro?', $table ),
			array( 'status' => 500 )
		);
	}

	$columns       = $wpdb->get_col( "SHOW COLUMNS FROM `{$table}`", 0 );
	$has_initiator = in_array( 'initiator_name', $columns, true );

	$from = $request->get_param( 'from' ) . ' 00:00:00';
	$to   = gmdate( 'Y-m-d', strtotime( $request->get_param( 'to' ) . ' +1 day' ) ) . ' 00:00:00';

	$initiator_select = $has_initiator ? 'initiator_name' : "''";
	$where            = 'date_sent >= %s AND date_sent < %s';
	$params           = array( $from, $to );

	$initiator = (string) $request->get_param( 'initiator' );
	if ( $initiator !== '' && $has_initiator ) {
		$where   .= ' AND initiator_name = %s';
		$params[] = $initiator;
	}

	$sql = "SELECT DATE_FORMAT(date_sent, '%%Y-%%m-%%d %%H:00:00') AS hour,
	               subject,
	               {$initiator_select} AS initiator,
	               COUNT(*) AS total
	        FROM `{$table}`
	        WHERE {$where}
	        GROUP BY hour, subject, initiator
	        ORDER BY hour ASC";

	$rows = $wpdb->get_results( $wpdb->prepare( $sql, $params ), ARRAY_A );
	if ( $wpdb->last_error ) {
		return new WP_Error( 'osnet_db_error', $wpdb->last_error, array( 'status' => 500 ) );
	}

	$oldest = $wpdb->get_var( "SELECT MIN(date_sent) FROM `{$table}`" );

	$out = array();
	foreach ( (array) $rows as $r ) {
		$out[] = array(
			'hour'      => $r['hour'],
			'subject'   => (string) $r['subject'],
			'initiator' => (string) $r['initiator'],
			'count'     => (int) $r['total'],
		);
	}

	return rest_ensure_response(
		array(
			'table'            => $table,
			'oldest_date_sent' => $oldest,
			'initiator_filter' => $has_initiator ? $initiator : '(columna initiator_name no disponible)',
			'rows'             => $out,
		)
	);
}
